package shell

import (
	"bufio"
	"context"
	"fmt"
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
