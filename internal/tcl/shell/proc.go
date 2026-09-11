package shell

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

const stderrLimit = 64 << 10

const DefaultOutputLimit = 32 << 20

var ErrOutputOverflow = errors.New("shell: child output queue overflowed")

type Proc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *pipeBuffer
	stderr *tailWriter

	done     chan struct{}
	waitErr  error
	killOnce sync.Once
	claimed  atomic.Bool

	closeTimeout time.Duration
}

type config struct {
	args         []string
	dir          string
	env          []string
	closeTimeout time.Duration
	outputLimit  int
}

type Option func(*config)

func WithArgs(args ...string) Option { return func(c *config) { c.args = args } }

func WithDir(dir string) Option { return func(c *config) { c.dir = dir } }

func WithEnv(env ...string) Option { return func(c *config) { c.env = env } }

func WithCloseTimeout(d time.Duration) Option { return func(c *config) { c.closeTimeout = d } }

func WithOutputLimit(n int) Option { return func(c *config) { c.outputLimit = n } }

func Start(ctx context.Context, path string, opts ...Option) (*Proc, error) {
	cfg := config{closeTimeout: 2 * time.Second, outputLimit: DefaultOutputLimit}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.outputLimit <= 0 {
		cfg.outputLimit = DefaultOutputLimit
	}

	cmd := exec.CommandContext(ctx, path, cfg.args...)
	cmd.Dir = cfg.dir
	if len(cfg.env) > 0 {
		cmd.Env = append(cmd.Environ(), cfg.env...)
	}

	p := &Proc{
		cmd:          cmd,
		stdout:       newPipeBuffer(cfg.outputLimit),
		stderr:       &tailWriter{limit: stderrLimit},
		done:         make(chan struct{}),
		closeTimeout: cfg.closeTimeout,
	}
	cmd.Stdout = p.stdout
	cmd.Stderr = p.stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	p.stdin = stdin

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {

		p.waitErr = cmd.Wait()
		p.stdout.close()
		close(p.done)
	}()
	return p, nil
}

var ErrClaimed = errors.New("shell: proc conversation already claimed")

func (p *Proc) Claim() error {
	if !p.claimed.CompareAndSwap(false, true) {
		return ErrClaimed
	}
	return nil
}

func (p *Proc) Stdin() io.WriteCloser { return p.stdin }

func (p *Proc) Stdout() io.Reader { return p.stdout }

func (p *Proc) StderrTail() []byte { return p.stderr.tail() }

func (p *Proc) Done() <-chan struct{} { return p.done }

func (p *Proc) Err() (err error, ok bool) {
	select {
	case <-p.done:
		return p.waitErr, true
	default:
		return nil, false
	}
}

func (p *Proc) Kill() {
	p.killOnce.Do(func() {
		_ = p.cmd.Process.Kill()
	})
	<-p.done
}

func (p *Proc) Close() error {
	_ = p.stdin.Close()
	select {
	case <-p.done:
	case <-time.After(p.closeTimeout):
		p.Kill()
	}
	return p.waitErr
}

type pipeBuffer struct {
	mu         sync.Mutex
	cond       *sync.Cond
	buf        bytes.Buffer
	limit      int
	closed     bool
	overflowed bool
}

func newPipeBuffer(limit int) *pipeBuffer {
	b := &pipeBuffer{limit: limit}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *pipeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.overflowed {

		return len(p), nil
	}
	if room := b.limit - b.buf.Len(); len(p) > room {

		if room > 0 {
			b.buf.Write(p[:room])
		}
		b.overflowed = true
		b.cond.Broadcast()
		return len(p), nil
	}
	n, err := b.buf.Write(p)
	b.cond.Broadcast()
	return n, err
}

func (b *pipeBuffer) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.buf.Len() == 0 && !b.closed && !b.overflowed {
		b.cond.Wait()
	}
	if b.buf.Len() > 0 {
		return b.buf.Read(p)
	}

	if b.overflowed {
		return 0, ErrOutputOverflow
	}
	return 0, io.EOF
}

func (b *pipeBuffer) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.cond.Broadcast()
}

type tailWriter struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buf.Write(p)
	if over := w.buf.Len() - w.limit; over > 0 {
		w.buf.Next(over)
	}
	return n, err
}

func (w *tailWriter) tail() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...)
}
