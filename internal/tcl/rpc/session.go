// Package rpc is dockhand's conversation with a Tcl shell: framed
// request/response over a shell.Proc.
//
// A Session loads a small dispatch loop into a bare tclsh; consumers
// register ops by supplying init scripts, and Call invokes them. Framing is
// length-prefixed in both directions, so no quoting or escaping logic
// exists on either side of the pipe, and replies are located by a sentinel,
// so stray output from the evaluated Tcl is noise rather than corruption.
//
// Every read is bounded, because the peer is a child process that may be
// wedged, confused, or writing something that is not this protocol at all,
// and a session outlives the exchange that opened it: the dispatch loop
// holds one for weeks. A line has a maximum length and a frame a maximum
// payload, the latter checked against the length the child advertised
// before the buffer for it exists. Exceeding either breaks the session
// permanently — ErrBrokenSession, which is the branch a pool takes to
// rebuild — rather than degrading a long-lived process by an amount
// nobody is measuring.
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

// DefaultLineLimit bounds a single line read from the child.
//
// Two kinds of line arrive here. A frame header is about thirty bytes and
// its shape is fixed. A noise line is whatever the evaluated Tcl printed,
// which in practice is mportinit's chatter and a ui callback's sentences —
// hundreds of bytes, and bounded in the end by the longest field a
// Portfile declares. Nothing legitimate approaches 1 MiB, so the bound is
// three orders of magnitude of headroom over the largest honest line and
// still a hard stop: without it a child that emits bytes and never a
// newline — wedged mid-write, or writing binary into a text stream — is
// read into memory until the machine gives out, and no timeout fires
// because the read is making progress the whole time.
const DefaultLineLimit = 1 << 20

// DefaultFrameLimit bounds a frame's payload, checked against the length
// the child advertised *before* the buffer for it is allocated: the number
// is the peer's, so trusting it with make() hands a wedged or confused
// child an allocation of its own choosing.
//
// The number is measured rather than asserted. The fattest payload the
// protocol carries is a port's evaluated metadata, whose bulk is the crate
// and vendor lists of the largest Portfiles: across the 20,076 Portfiles
// in macports-ports the median is 1.2 KiB, the 99th percentile 31 KiB and
// the largest 139 KiB. This package's own tests round-trip a megabyte to
// prove the framing is length-honest rather than line-honest. 16 MiB is
// two orders of magnitude above the largest real payload and sixteen times
// the largest exercised one, and sits below the shell package's default
// output queue bound so a legitimate frame can be queued whole.
const DefaultFrameLimit = 16 << 20

const sentinel = "TCLRPC1 "

// ErrHandshake reports that a new session's dispatch loop never answered
// its first ping within the handshake bound — the proc was not a fresh
// tclsh with an untouched stdin, or its init scripts wedged.
var ErrHandshake = errors.New("rpc: no handshake")

// ErrBrokenSession reports a session whose transport has failed
// permanently; the underlying process is dead and every call returns this,
// the one that broke it included. Callers that pool sessions branch on it
// to rebuild — "this evaluator is dead, get a new one" is a check against
// this sentinel, never a search for words in a message. The cause stays
// wrapped inside for diagnosis.
var ErrBrokenSession = errors.New("rpc: session broken")

// ErrLineLimit reports that the child wrote a line longer than the
// session's line limit. It breaks the session: a stream that has to be
// abandoned mid-line cannot be resynchronized, because the only thing that
// marks a frame boundary is the newline that never came.
var ErrLineLimit = errors.New("rpc: line limit exceeded")

// ErrFrameLimit reports a frame whose advertised payload exceeds the
// session's frame limit. It breaks the session, and it is raised before
// the payload is allocated: a length this large is evidence the child is
// no longer speaking the protocol, so the number is not to be acted on.
var ErrFrameLimit = errors.New("rpc: frame limit exceeded")

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
// A transport failure — an unreadable frame, a dead process, a cancelled
// context, a line or a frame past its limit — breaks the session
// permanently: the underlying process is killed, the call that discovered
// it returns ErrBrokenSession, and so does every call after it, without
// touching the pipes again. Handler errors, by contrast, arrive as
// CallError and leave the session healthy.
type Session struct {
	proc *shell.Proc
	r    *bufio.Reader

	// The transport bounds are per-session rather than package-global so
	// a consumer with genuinely larger replies can raise them without
	// raising them for the evaluator that sits beside it.
	lineLimit  int
	frameLimit int

	mu     sync.Mutex
	broken error
	noise  bytes.Buffer
}

const defaultHandshakeTimeout = 30 * time.Second

type config struct {
	inits      []string
	handshake  time.Duration
	lineLimit  int
	frameLimit int
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

// WithLineLimit overrides DefaultLineLimit for this session. A
// non-positive n leaves the default in place; there is no setting for an
// unbounded read.
func WithLineLimit(n int) Option { return func(c *config) { c.lineLimit = n } }

// WithFrameLimit overrides DefaultFrameLimit for this session — raised by
// a consumer whose ops genuinely return more than the default, lowered to
// hold a suspect child on a short leash. A non-positive n leaves the
// default in place; there is no setting for an unbounded frame.
func WithFrameLimit(n int) Option { return func(c *config) { c.frameLimit = n } }

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
	cfg := config{
		handshake:  defaultHandshakeTimeout,
		lineLimit:  DefaultLineLimit,
		frameLimit: DefaultFrameLimit,
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.lineLimit <= 0 {
		cfg.lineLimit = DefaultLineLimit
	}
	if cfg.frameLimit <= 0 {
		cfg.frameLimit = DefaultFrameLimit
	}
	if err := proc.Claim(); err != nil {
		return nil, fmt.Errorf("rpc: %w", err)
	}
	hctx, cancel := context.WithTimeout(ctx, cfg.handshake)
	defer cancel()
	s, err := newSession(hctx, proc, cfg)
	if err != nil {
		proc.Kill()
		if hctx.Err() != nil && ctx.Err() == nil {
			return nil, fmt.Errorf("%w within %v: %w (the proc must be a fresh tclsh with an untouched stdin)", ErrHandshake, cfg.handshake, err)
		}
		return nil, err
	}
	return s, nil
}

func newSession(ctx context.Context, proc *shell.Proc, cfg config) (*Session, error) {
	s := &Session{
		proc:       proc,
		r:          bufio.NewReader(proc.Stdout()),
		lineLimit:  cfg.lineLimit,
		frameLimit: cfg.frameLimit,
	}
	// Definitions first, then init scripts (whose registrations need the
	// tclrpc namespace), then the loop itself: nothing reads a frame
	// until every handler is in place.
	if _, err := io.WriteString(proc.Stdin(), loopScript); err != nil {
		return nil, fmt.Errorf("rpc: loading loop: %w", err)
	}
	for _, init := range cfg.inits {
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
//
// A transport failure returns an error wrapping ErrBrokenSession, and so
// does every later call: once the session is known unusable no further
// I/O is attempted on it, so a caller that missed the break still gets a
// verdict rather than a hang on a dead process.
func (s *Session) Call(ctx context.Context, op string, args ...string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken != nil {
		return "", s.broken
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
				return "", s.breakSession(res.err)
			}
			return "", res.err
		}
		return res.payload, nil
	case <-ctx.Done():
		// Mid-frame abandonment is unrecoverable; the process goes too.
		return "", s.breakSession(ctx.Err())
	}
}

// breakSession records the cause, kills the process, and returns the error
// every call on this session now gets — the breaking one included, so the
// caller who witnessed the failure branches on the same sentinel as the
// caller who arrives afterwards. The cause is kept wrapped: context
// deadlines, shell.ErrOutputOverflow and the limit sentinels all stay
// visible to errors.Is through it.
func (s *Session) breakSession(cause error) error {
	if s.broken == nil {
		s.broken = fmt.Errorf("%w: %w", ErrBrokenSession, cause)
	}
	s.proc.Kill()
	return s.broken
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
		line, err := readLine(s.r, s.lineLimit)
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
		if n > s.frameLimit {
			// Checked before the allocation below, not after it: n is
			// the child's number, and make() is where trusting it costs
			// the address space.
			return "", noise.Bytes(), fmt.Errorf("%w: frame advertises %d bytes, limit is %d", ErrFrameLimit, n, s.frameLimit)
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

// readLine reads one newline-terminated line, at most limit bytes long,
// returning it with its newline still attached.
//
// bufio.Reader.ReadString would be shorter and is what this replaces, but
// it grows a buffer for as long as the child keeps writing: the size of
// the read is the child's choice, not ours. ReadSlice hands back what fits
// in the reader's fixed window instead, so the accumulation happens here
// where it can be counted, and a line that reaches the limit without a
// newline is refused rather than allocated. Nothing tries to resynchronize
// after that: the newline is the only frame boundary the protocol has, so
// a stream abandoned mid-line has no next frame to find.
func readLine(r *bufio.Reader, limit int) (string, error) {
	var line strings.Builder
	for {
		frag, err := r.ReadSlice('\n')
		if line.Len()+len(frag) > limit {
			return "", fmt.Errorf("%w: %d bytes with no newline, limit is %d", ErrLineLimit, line.Len()+len(frag), limit)
		}
		line.Write(frag)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return line.String(), err
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
