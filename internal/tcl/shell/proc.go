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

// stderrLimit bounds the retained tail of a process's stderr. Only the most
// recent bytes are kept, as diagnostic context for failures.
const stderrLimit = 64 << 10

// Proc is a running tclsh child process. It provides pipes and lifecycle
// and nothing else.
//
// Proc is not safe for concurrent pipe use, and no lock here could make it
// so: interleaved writes are incoherent unless something knows where
// messages begin and end, and message boundaries are protocol knowledge.
// Serialization belongs to whatever owns the conversation (see rpc.Session,
// which holds exactly that mutex). A Proc carries one conversation.
//
// Output is routed through exec's own copy goroutines into in-process
// buffers rather than through OS pipes handed to the caller. That closes a
// data-loss race: Wait on a command with StdoutPipe tears the pipe down at
// exit, which can discard buffered output the consumer has not read yet.
// With writers, Wait returns only after every byte has been copied, and
// stdout is closed for readers only after that.
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
}

// Option configures Start.
type Option func(*config)

// WithArgs passes arguments to the shell.
func WithArgs(args ...string) Option { return func(c *config) { c.args = args } }

// WithDir sets the working directory.
func WithDir(dir string) Option { return func(c *config) { c.dir = dir } }

// WithEnv appends environment variables (KEY=value) to the inherited
// environment.
func WithEnv(env ...string) Option { return func(c *config) { c.env = env } }

// WithCloseTimeout sets how long Close waits for a graceful exit after
// closing stdin before killing the process. Default two seconds.
func WithCloseTimeout(d time.Duration) Option { return func(c *config) { c.closeTimeout = d } }

// Start launches a tclsh at the given binary path, reading Tcl commands
// from stdin. The process is killed when ctx is cancelled.
//
// There is deliberately no script-file mode: a script-owned-stdin process
// is a different capability from a command-stream one, and will arrive as a
// distinct type when a consumer exists.
func Start(ctx context.Context, path string, opts ...Option) (*Proc, error) {
	cfg := config{closeTimeout: 2 * time.Second}
	for _, o := range opts {
		o(&cfg)
	}

	cmd := exec.CommandContext(ctx, path, cfg.args...)
	cmd.Dir = cfg.dir
	if len(cfg.env) > 0 {
		cmd.Env = append(cmd.Environ(), cfg.env...)
	}

	p := &Proc{
		cmd:          cmd,
		stdout:       newPipeBuffer(),
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
		// Wait returns only after the stdout and stderr copiers finish, so
		// closing the buffer here can never strand unread output.
		p.waitErr = cmd.Wait()
		p.stdout.close()
		close(p.done)
	}()
	return p, nil
}

// ErrClaimed reports that a proc's conversation slot is already taken.
var ErrClaimed = errors.New("shell: proc conversation already claimed")

// Claim reserves the proc's single conversation slot. A Proc carries one
// conversation (see the type comment), so whatever takes over its pipes —
// an rpc.Session, or any future pattern — claims first; a second claim
// returns ErrClaimed. Claiming does not touch the process, and a refused
// claimant must not either: the proc belongs to its first owner.
func (p *Proc) Claim() error {
	if !p.claimed.CompareAndSwap(false, true) {
		return ErrClaimed
	}
	return nil
}

// Stdin is the process's standard input. Closing it asks tclsh to exit.
func (p *Proc) Stdin() io.WriteCloser { return p.stdin }

// Stdout is the process's standard output. Reads block until output
// arrives, and see io.EOF only after the process has exited with all output
// delivered.
func (p *Proc) Stdout() io.Reader { return p.stdout }

// StderrTail returns a copy of the most recent stderr output.
func (p *Proc) StderrTail() []byte { return p.stderr.tail() }

// Done is closed when the process has exited and all output is delivered.
func (p *Proc) Done() <-chan struct{} { return p.done }

// Err reports the process's exit error. ok is false while the process is
// still running; a clean exit is (nil, true).
func (p *Proc) Err() (err error, ok bool) {
	select {
	case <-p.done:
		return p.waitErr, true
	default:
		return nil, false
	}
}

// Kill terminates the process immediately and waits for it to be reaped.
func (p *Proc) Kill() {
	p.killOnce.Do(func() {
		_ = p.cmd.Process.Kill()
	})
	<-p.done
}

// Close asks the process to exit by closing stdin, waits up to the close
// timeout, and kills it if it has not exited. It returns the exit error, so
// a shell that leaves gracefully yields nil.
func (p *Proc) Close() error {
	_ = p.stdin.Close()
	select {
	case <-p.done:
	case <-time.After(p.closeTimeout):
		p.Kill()
	}
	return p.waitErr
}

// pipeBuffer is an unbounded write-never-blocks buffer with blocking reads:
// the child's output lands here at whatever rate it is produced, bounded by
// the process's lifetime, and readers drain at their own pace.
type pipeBuffer struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    bytes.Buffer
	closed bool
}

func newPipeBuffer() *pipeBuffer {
	b := &pipeBuffer{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *pipeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buf.Write(p)
	b.cond.Broadcast()
	return n, err
}

func (b *pipeBuffer) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.buf.Len() == 0 && !b.closed {
		b.cond.Wait()
	}
	if b.buf.Len() > 0 {
		return b.buf.Read(p)
	}
	return 0, io.EOF
}

func (b *pipeBuffer) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.cond.Broadcast()
}

// tailWriter retains the last limit bytes written.
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
