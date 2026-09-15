package shell_test

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/stretchr/testify/require"
)

func start(t *testing.T, script string, opts ...shell.Option) *shell.Proc {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	opts = append([]shell.Option{shell.WithArgs("-c", script)}, opts...)
	p, err := shell.Start(ctx, "/bin/sh", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func TestExitPreservesOutputAndStatus(t *testing.T) {
	p := start(t, "printf output; printf diagnostic >&2; exit 7")
	output, err := io.ReadAll(p.Stdout())
	require.NoError(t, err)
	require.Equal(t, "output", string(output))
	require.Error(t, p.Close())
	err, done := p.Err()
	require.True(t, done)
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 7, exit.ExitCode())
	require.Equal(t, "diagnostic", string(p.StderrTail()))
	tail := p.StderrTail()
	tail[0] = 'X'
	require.Equal(t, "diagnostic", string(p.StderrTail()))
}

func TestCloseDeliversEOFAndClaimIsExclusive(t *testing.T) {
	p := start(t, "cat; printf done")
	require.NoError(t, p.Claim())
	require.ErrorIs(t, p.Claim(), shell.ErrClaimed)
	_, err := io.WriteString(p.Stdin(), "input\n")
	require.NoError(t, err)
	require.NoError(t, p.Close())
	output, err := io.ReadAll(p.Stdout())
	require.NoError(t, err)
	require.Equal(t, "input\ndone", string(output))
	require.NoError(t, p.Close())
}

func TestBoundedOutputDoesNotBlockExit(t *testing.T) {
	p := start(t, "printf 123456789", shell.WithOutputLimit(4))
	select {
	case <-p.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("unread overflow prevented process exit")
	}
	output, err := io.ReadAll(p.Stdout())
	require.Equal(t, "1234", string(output))
	require.ErrorIs(t, err, shell.ErrOutputOverflow)
	require.NoError(t, p.Close())
}

func TestStderrRetainsTail(t *testing.T) {
	p := start(t, "i=0; while [ $i -lt 7000 ]; do printf 0123456789 >&2; i=$((i+1)); done; printf END >&2")
	require.NoError(t, p.Close())
	tail := string(p.StderrTail())
	require.Len(t, tail, 64<<10)
	require.True(t, strings.HasSuffix(tail, "0123456789END"))
}

func TestCancellationUnblocksReader(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	p, err := shell.Start(ctx, "/bin/sh", shell.WithArgs("-c", "read value"))
	require.NoError(t, err)
	t.Cleanup(func() { cancel(); _ = p.Close() })
	result := make(chan error, 1)
	go func() { _, err := io.ReadAll(p.Stdout()); result <- err }()
	cancel()
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("reader did not stop after cancellation")
	}
	require.Error(t, p.Close())
}

func TestCloseKillsChildIgnoringEOF(t *testing.T) {
	p := start(t, "while :; do read value || :; done", shell.WithCloseTimeout(20*time.Millisecond))
	result := make(chan error, 1)
	go func() { result <- p.Close() }()
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not terminate child")
	}
	p.Kill()
	_, done := p.Err()
	require.True(t, done)
}

func TestStartFailures(t *testing.T) {
	_, err := shell.Start(t.Context(), "/nonexistent/dockhand-tcl")
	require.Error(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = shell.Start(ctx, "/bin/sh")
	require.True(t, errors.Is(err, context.Canceled))
}
