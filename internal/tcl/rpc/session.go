// Package rpc is dockhand's conversation with a Tcl shell: framed
// request/response over a shell.Proc.
//
// A Session loads a small dispatch loop into a bare tclsh; consumers
// register ops by supplying init scripts, and Call invokes them. Framing is
// length-prefixed in both directions, so no quoting or escaping logic
// exists on either side of the pipe, and replies are located by a sentinel,
// so stray output from the evaluated Tcl is noise rather than corruption.
//
// The protocol is generic over which tclsh runs it — plain or port-tclsh —
// and knows nothing about MacPorts; MacPorts semantics arrive as init
// scripts owned by higher layers (see macports/eval).
package rpc

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/herbygillot/dockhand/internal/tcl/shell"
)

//go:embed loop.tcl
var loopScript string

// noiseLimit bounds the retained tail of non-frame stdout seen between
// replies — output from evaluated Tcl that wrote to stdout directly.
const noiseLimit = 64 << 10

const sentinel = "TCLRPC1 "

// ErrHandshake reports that a new session's dispatch loop never answered
// its first ping within the handshake bound — the proc was not a fresh
// tclsh with an untouched stdin, or its init scripts wedged.
var ErrHandshake = errors.New("rpc: no handshake")

// ErrBroken reports a session whose transport has failed permanently; the
// underlying process is dead and every call returns this. Callers that pool
// sessions branch on it to rebuild.
var ErrBroken = errors.New("rpc: session broken")

// CallError is a handler-reported failure: the Tcl side caught an error and
// framed it. The session remains usable after one.
type CallError struct {
	Msg string
}

func (e CallError) Error() string { return "tcl: " + e.Msg }

// Session is framed request/response over a Proc. It loads the dispatch
// loop plus any init scripts into the shell; handlers registered by those
// scripts become callable ops. One call is outstanding at a time: the
// serialization mutex for the underlying pipes lives here, because only the
// protocol layer knows message boundaries. A Session takes exclusive
// ownership of its Proc's pipes.
//
// A transport failure (unreadable frame, dead process, cancelled context)
// breaks the session permanently: the underlying process is killed and
// every subsequent call fails. Handler errors, by contrast, arrive as
// CallError and leave the session healthy.
type Session struct {
	proc *shell.Proc
	r    *bufio.Reader

	mu     sync.Mutex
	broken error
	noise  bytes.Buffer
}

const defaultHandshakeTimeout = 30 * time.Second

type config struct {
	inits     []string
	handshake time.Duration
}

// Option configures New.
type Option func(*config)

// WithInit evaluates the given Tcl scripts before the dispatch loop starts,
// which is how consumers register their ops.
func WithInit(scripts ...string) Option {
	return func(c *config) { c.inits = append(c.inits, scripts...) }
}

// WithHandshakeTimeout bounds how long New waits for the loop to answer its
// first ping. The default is generous; init scripts that legitimately take
// longer (heavy package loads) raise it here.
func WithHandshakeTimeout(d time.Duration) Option {
	return func(c *config) { c.handshake = d }
}

// New loads the dispatch loop into a fresh Proc, after evaluating any
// WithInit scripts, and pings the loop before returning, so a non-nil
// Session is known live. The handshake is bounded regardless of ctx: a proc
// whose stdin has been written to, or that is otherwise not a fresh command
// stream, fails here loudly instead of hanging.
//
// New claims the proc's conversation slot first; a proc already claimed is
// refused without being touched, since it belongs to its first owner. On
// every other failure New takes ownership and kills the proc, so callers
// never inherit a half-initialized shell.
func New(ctx context.Context, proc *shell.Proc, opts ...Option) (*Session, error) {
	cfg := config{handshake: defaultHandshakeTimeout}
	for _, o := range opts {
		o(&cfg)
	}
	if err := proc.Claim(); err != nil {
		return nil, fmt.Errorf("rpc: %w", err)
	}
	hctx, cancel := context.WithTimeout(ctx, cfg.handshake)
	defer cancel()
	s, err := newSession(hctx, proc, cfg.inits)
	if err != nil {
		proc.Kill()
		if hctx.Err() != nil && ctx.Err() == nil {
			return nil, fmt.Errorf("%w within %v: %w (the proc must be a fresh tclsh with an untouched stdin)", ErrHandshake, cfg.handshake, err)
		}
		return nil, err
	}
	return s, nil
}

func newSession(ctx context.Context, proc *shell.Proc, initScripts []string) (*Session, error) {
	s := &Session{proc: proc, r: bufio.NewReader(proc.Stdout())}
	// Definitions first, then init scripts (whose registrations need the
	// tclrpc namespace), then the loop itself: nothing reads a frame
	// until every handler is in place.
	if _, err := io.WriteString(proc.Stdin(), loopScript); err != nil {
		return nil, fmt.Errorf("rpc: loading loop: %w", err)
	}
	for _, init := range initScripts {
		if _, err := io.WriteString(proc.Stdin(), init+"\n"); err != nil {
			return nil, fmt.Errorf("rpc: loading init script: %w", err)
		}
	}
	if _, err := io.WriteString(proc.Stdin(), "::tclrpc::loop\n"); err != nil {
		return nil, fmt.Errorf("rpc: starting loop: %w", err)
	}
	if _, err := s.Call(ctx, "ping"); err != nil {
		return nil, fmt.Errorf("rpc: session did not come up: %w", err)
	}
	return s, nil
}

// Noise returns a copy of recent between-frame stdout — output the
// evaluated Tcl printed directly. Diagnostic, best-effort, bounded.
func (s *Session) Noise() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.noise.Bytes()...)
}

// Call invokes the named op with arguments and returns the reply payload.
// Arguments and results may contain any bytes valid in UTF-8, newlines and
// Tcl syntax included; nothing is quoted on the wire.
func (s *Session) Call(ctx context.Context, op string, args ...string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken != nil {
		return "", fmt.Errorf("%w: %w", ErrBroken, s.broken)
	}

	type result struct {
		payload string
		noise   []byte
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		payload, noise, err := s.roundTrip(op, args)
		ch <- result{payload, noise, err}
	}()

	select {
	case res := <-ch:
		// Noise is merged here, under the session mutex, rather than
		// written by the I/O goroutine: an abandoned goroutine must never
		// touch shared state after Call has returned.
		s.noise.Write(res.noise)
		if over := s.noise.Len() - noiseLimit; over > 0 {
			s.noise.Next(over)
		}
		if res.err != nil {
			var ce CallError
			if !errors.As(res.err, &ce) {
				s.breakSession(res.err)
			}
			return "", res.err
		}
		return res.payload, nil
	case <-ctx.Done():
		// Mid-frame abandonment is unrecoverable; the process goes too.
		s.breakSession(ctx.Err())
		return "", ctx.Err()
	}
}

func (s *Session) breakSession(err error) {
	s.broken = err
	s.proc.Kill()
}

func (s *Session) roundTrip(op string, args []string) (string, []byte, error) {
	var req bytes.Buffer
	fmt.Fprintf(&req, "CALL %d\n", 1+len(args))
	for _, a := range append([]string{op}, args...) {
		fmt.Fprintf(&req, "%d\n%s\n", len(a), a)
	}
	if _, err := s.proc.Stdin().Write(req.Bytes()); err != nil {
		return "", nil, fmt.Errorf("rpc: write: %w", err)
	}

	var noise bytes.Buffer
	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			return "", noise.Bytes(), fmt.Errorf("rpc: read: %w (stderr: %q)", err, s.proc.StderrTail())
		}
		if !strings.HasPrefix(line, sentinel) {
			noise.WriteString(line)
			if over := noise.Len() - noiseLimit; over > 0 {
				noise.Next(over)
			}
			continue
		}
		fields := strings.Fields(strings.TrimSuffix(line, "\n"))
		if len(fields) != 3 {
			return "", noise.Bytes(), fmt.Errorf("rpc: malformed frame header %q", line)
		}
		n, err := strconv.Atoi(fields[2])
		if err != nil || n < 0 {
			return "", noise.Bytes(), fmt.Errorf("rpc: malformed frame length %q", line)
		}
		payload := make([]byte, n+1)
		if _, err := io.ReadFull(s.r, payload); err != nil {
			return "", noise.Bytes(), fmt.Errorf("rpc: short frame: %w", err)
		}
		body := string(payload[:n])
		switch fields[1] {
		case "ok":
			return body, noise.Bytes(), nil
		case "err":
			return "", noise.Bytes(), CallError{Msg: body}
		default:
			return "", noise.Bytes(), fmt.Errorf("rpc: unknown frame status %q", fields[1])
		}
	}
}

// Close ends the session and its process. Safe after breakage.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken != nil {
		return nil
	}
	return s.proc.Close()
}
