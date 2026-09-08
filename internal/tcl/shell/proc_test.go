package shell

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/testenv"
)

// startProc launches a plain tclsh in stdin-command mode, ready for
// write-command/read-reply exchanges.
func startProc(t *testing.T, opts ...Option) (*Proc, *bufio.Reader) {
	t.Helper()
	path := testenv.Tclsh(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p, err := Start(ctx, path, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { p.Kill() })
	return p, bufio.NewReader(p.Stdout())
}

func send(t *testing.T, p *Proc, cmd string) {
	t.Helper()
	_, err := fmt.Fprintln(p.Stdin(), cmd)
	require.NoError(t, err)
}

func readLine(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	line, err := r.ReadString('\n')
	require.NoError(t, err)
	return strings.TrimRight(line, "\n")
}

func TestProcEcho(t *testing.T) {
	p, r := startProc(t)
	send(t, p, "fconfigure stdout -buffering line")
	send(t, p, "puts [string toupper hello]")
	require.Equal(t, "HELLO", readLine(t, r))
}

func TestProcCleanExitOnStdinClose(t *testing.T) {
	p, r := startProc(t)
	send(t, p, "fconfigure stdout -buffering line")
	send(t, p, "puts ready")
	readLine(t, r)
	require.NoError(t, p.Close())
	err, ok := p.Err()
	require.True(t, ok, "exited process must report an exit status")
	require.NoError(t, err)
}

func TestProcKill(t *testing.T) {
	p, _ := startProc(t)
	_, ok := p.Err()
	require.False(t, ok, "running process must not report an exit status")
	p.Kill()
	err, ok := p.Err()
	require.True(t, ok)
	require.Error(t, err, "killed process must not report a clean exit")
}

func TestProcContextCancel(t *testing.T) {
	path := testenv.Tclsh(t)
	ctx, cancel := context.WithCancel(context.Background())
	p, err := Start(ctx, path)
	require.NoError(t, err)
	cancel()
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process survived context cancellation")
	}
}

func TestProcStderrTail(t *testing.T) {
	p, r := startProc(t)
	send(t, p, "fconfigure stdout -buffering line")
	send(t, p, `puts stderr oops; puts marker`)
	readLine(t, r) // marker: stderr write has happened by now
	require.NoError(t, p.Close())
	require.Contains(t, string(p.StderrTail()), "oops")
}

func TestProcOutputLimitPoisonsTheQueue(t *testing.T) {
	const limit = 4 << 10
	p, _ := startProc(t, WithOutputLimit(limit))
	send(t, p, "fconfigure stdout -buffering none")
	send(t, p, "puts -nonewline [string repeat x 200000]")
	// The child announces on stderr that it is done writing, so nothing
	// here reads stdout while it is still filling: an owner draining
	// concurrently would keep the queue under its bound, which is the
	// healthy case and not the one under test.
	send(t, p, "puts stderr done; flush stderr")
	require.Eventually(t, func() bool {
		return strings.Contains(string(p.StderrTail()), "done")
	}, 10*time.Second, 20*time.Millisecond, "child never finished writing")

	n, err := io.Copy(io.Discard, p.Stdout())
	require.ErrorIs(t, err, ErrOutputOverflow)
	require.LessOrEqual(t, n, int64(limit), "queue delivered more than it may hold")

	// Sticky: the stream lost bytes, so it never becomes readable again,
	// and it never degrades into a clean EOF that a parser would take for
	// the end of a message.
	_, err = p.Stdout().Read(make([]byte, 1))
	require.ErrorIs(t, err, ErrOutputOverflow)
	p.Kill()
	_, err = p.Stdout().Read(make([]byte, 1))
	require.ErrorIs(t, err, ErrOutputOverflow, "overflow must outrank EOF after the process exits")
}

func TestPipeBufferOverflowWakesBlockedReader(t *testing.T) {
	// The reader parks on an empty queue and is woken by the overflow
	// itself. Without that broadcast it would wait for a write that the
	// poisoned queue will never deliver — which is the stall a limit is
	// supposed to prevent, arriving by a different door.
	b := newPipeBuffer(4)
	woke := make(chan error, 1)
	go func() {
		buf := make([]byte, 8)
		for {
			if _, err := b.Read(buf); err != nil {
				woke <- err
				return
			}
		}
	}()
	_, err := b.Write([]byte("123456789"))
	require.NoError(t, err, "the buffer never fails its writer, even past the limit")
	select {
	case err := <-woke:
		require.ErrorIs(t, err, ErrOutputOverflow)
	case <-time.After(5 * time.Second):
		t.Fatal("a reader blocked on an empty queue was not woken by overflow")
	}
}

func TestPipeBufferDeliversThePrefixBeforeOverflowing(t *testing.T) {
	b := newPipeBuffer(4)
	n, err := b.Write([]byte("abcdef"))
	require.NoError(t, err, "an overflowing write must not fail the writer")
	require.Equal(t, 6, n, "a short write would stop exec's copier and wedge the child")

	got := make([]byte, 8)
	n, err = b.Read(got)
	require.NoError(t, err)
	require.Equal(t, "abcd", string(got[:n]), "the bytes that fit are still the child's last honest output")

	_, err = b.Read(got)
	require.ErrorIs(t, err, ErrOutputOverflow)
	b.close()
	_, err = b.Read(got)
	require.ErrorIs(t, err, ErrOutputOverflow)
}

func TestProcOutputLimitAdmitsOrdinaryOutput(t *testing.T) {
	// The default bound is far above anything MacPorts produces; a
	// megabyte in one line has to pass untouched.
	p, r := startProc(t)
	send(t, p, "fconfigure stdout -buffering line")
	send(t, p, "puts [string repeat x 1000000]")
	require.Len(t, readLine(t, r), 1000000)
}

func TestProcClaim(t *testing.T) {
	p, _ := startProc(t)
	require.NoError(t, p.Claim())
	require.ErrorIs(t, p.Claim(), ErrClaimed)
	select {
	case <-p.Done():
		t.Fatal("refused claim touched the process")
	default:
	}
}
