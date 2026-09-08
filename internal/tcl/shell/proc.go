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

// DefaultOutputLimit bounds the child-output queue: bytes the child has
// written to stdout that its owner has not read yet.
//
// A bound is needed because a Proc outlives any one exchange. The long
// road holds a single evaluator open for the life of a dispatch loop —
// weeks — and the queue is drained only while its owner is mid-exchange.
// Everything a child prints between exchanges (a Portfile's own puts, a
// ui callback, a package's load-time chatter) therefore lands in a queue
// nobody is reading, and an unbounded queue turns a chatty or wedged
// child into a leak with no ceiling and no diagnosis: the process grows
// until the machine notices, and the failure names neither the child nor
// the reason.
//
// The number is measured rather than asserted. The largest payload the
// protocol above ever carries is a port's evaluated metadata, whose bulk
// is the crate and vendor lists of the fattest Portfiles: across the
// 20,076 Portfiles in macports-ports the median is 1.2 KiB, the 99th
// percentile 31 KiB, and the largest 139 KiB. The rpc layer above accepts
// frames up to its own 16 MiB bound, so 32 MiB holds any legitimate reply
// whole — twice over — while still costing a runaway child a definite
// ceiling instead of the machine's memory.
const DefaultOutputLimit = 32 << 20

// ErrOutputOverflow reports that a child wrote more to stdout than its
// output queue may hold, so the stream has lost bytes and cannot be
// parsed any further. It is permanent for the Proc: every read after the
// queued prefix is drained returns it. A protocol layer above translates
// it into whatever "this conversation is over" means there.
var ErrOutputOverflow = errors.New("shell: child output queue overflowed")

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
	outputLimit  int
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

// WithOutputLimit bounds the child-output queue at n bytes, overriding
// DefaultOutputLimit. Raise it for a conversation whose replies are
// genuinely larger; lower it to hold a suspect child on a short leash. A
// non-positive n is not a way to ask for no bound — there is no such
// setting — and leaves the default in place.
func WithOutputLimit(n int) Option { return func(c *config) { c.outputLimit = n } }

// Start launches a tclsh at the given binary path, reading Tcl commands
// from stdin. The process is killed when ctx is cancelled.
//
// There is deliberately no script-file mode: a script-owned-stdin process
// is a different capability from a command-stream one, and will arrive as a
// distinct type when a consumer exists.
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
//
// Output the reader has not taken yet is queued in process, bounded by the
// proc's output limit. A child that outruns that bound poisons the queue:
// the bytes already queued are still delivered, and every read after them
// returns ErrOutputOverflow, permanently. A reader parked on an empty
// queue is woken by the overflow itself rather than left to wait for a
// write that will never come.
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

// pipeBuffer is the child-output queue: a write-never-blocks buffer with
// blocking reads, so the child's output lands here at whatever rate it is
// produced and readers drain at their own pace. It holds at most limit
// unread bytes (see DefaultOutputLimit).
//
// Past the bound the queue is poisoned rather than the writer failed. A
// failing Write would stop exec's output copier, which leaves the child
// blocked on a full OS pipe waiting for a reader that is never coming —
// a stall nothing observes and nothing times out, which is exactly the
// failure a limit is here to prevent. So writes always succeed: what
// overflows is dropped, the queue records that it happened, and the fact
// surfaces on the read side where a caller is already checking errors.
//
// Overflow is sticky and broadcasts, so a reader parked on an empty queue
// learns of it at once instead of at the next write. That also means no
// reader can be parked while the queue is poisoned, which is why the
// discard path has nobody left to wake.
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
		// Keep draining the child's pipe so it runs to its own end (or
		// to the kill its owner is about to issue) rather than wedging
		// on a pipe nobody empties. The bytes go nowhere.
		return len(p), nil
	}
	if room := b.limit - b.buf.Len(); len(p) > room {
		// The prefix that fits is still delivered: it is the last honest
		// output of the child, and the most useful thing a diagnostic
		// has to work with.
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
	// Overflow outranks EOF: a stream that lost bytes did not end, and a
	// reader told otherwise would take a truncated frame for a complete
	// one.
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
