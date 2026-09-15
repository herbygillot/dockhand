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

const noiseLimit = 64 << 10

const DefaultLineLimit = 1 << 20

const DefaultFrameLimit = 16 << 20

const sentinel = "TCLRPC1 "

var ErrHandshake = errors.New("rpc: no handshake")

var ErrBrokenSession = errors.New("rpc: session broken")

var ErrLineLimit = errors.New("rpc: line limit exceeded")

var ErrFrameLimit = errors.New("rpc: frame limit exceeded")

type CallError struct {
	Msg string
}

func (e CallError) Error() string { return "tcl: " + e.Msg }

type Session struct {
	proc *shell.Proc
	r    *bufio.Reader

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

type Option func(*config)

func WithInit(scripts ...string) Option {
	return func(c *config) { c.inits = append(c.inits, scripts...) }
}

func WithHandshakeTimeout(d time.Duration) Option {
	return func(c *config) { c.handshake = d }
}

func WithLineLimit(n int) Option { return func(c *config) { c.lineLimit = n } }

func WithFrameLimit(n int) Option { return func(c *config) { c.frameLimit = n } }

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
	stop := context.AfterFunc(hctx, proc.Kill)
	defer stop()
	s, err := newSession(hctx, proc, cfg)
	if err != nil {
		proc.Kill()
		if hctx.Err() != nil && ctx.Err() == nil {
			return nil, fmt.Errorf("%w within %v: %w (the proc must be a fresh tclsh with an untouched stdin)", ErrHandshake, cfg.handshake, errors.Join(err, hctx.Err()))
		}
		return nil, errors.Join(err, hctx.Err())
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

func (s *Session) Noise() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.noise.Bytes()...)
}

func (s *Session) Call(ctx context.Context, op string, args ...string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken != nil {
		return "", s.broken
	}
	if err := ctx.Err(); err != nil {
		return "", err
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

		return "", s.breakSession(ctx.Err())
	}
}

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

			return "", noise.Bytes(), fmt.Errorf("%w: frame advertises %d bytes, limit is %d", ErrFrameLimit, n, s.frameLimit)
		}
		payload := make([]byte, n+1)
		if _, err := io.ReadFull(s.r, payload); err != nil {
			return "", noise.Bytes(), fmt.Errorf("rpc: short frame: %w", err)
		}
		if payload[n] != '\n' {
			return "", noise.Bytes(), fmt.Errorf("rpc: malformed frame delimiter %q", payload[n])
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

func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken != nil {
		return nil
	}
	return s.proc.Close()
}
