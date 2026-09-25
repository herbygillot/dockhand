package rpc_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/tcl/rpc"
	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/stretchr/testify/require"
)

// process starts the tclsh on PATH, or DOCKHAND_TEST_TCLSH, which can name
// a Tcl 9 shell such as Base master's port-tclsh.
func process(t *testing.T) *shell.Proc {
	t.Helper()
	path := os.Getenv("DOCKHAND_TEST_TCLSH")
	if path == "" {
		var err error
		if path, err = exec.LookPath("tclsh"); err != nil {
			t.Skip("tclsh is required for protocol integration tests")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	p, err := shell.Start(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func session(t *testing.T, opts ...rpc.Option) (*rpc.Session, *shell.Proc) {
	t.Helper()
	p := process(t)
	s, err := rpc.New(t.Context(), p, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s, p
}

func TestRoundTripAndRecoverableTclError(t *testing.T) {
	s, p := session(t, rpc.WithInit("proc echo {value} {return $value}; ::tclrpc::register echo echo"))
	for _, input := range []string{"", "a\nb\x00c", "é日本語", "{[value]} $x \\"} {
		output, err := s.Call(t.Context(), "echo", input)
		require.NoError(t, err)
		require.Equal(t, input, output)
	}
	_, err := s.Call(t.Context(), "eval", "error {fixture error}")
	var callError rpc.CallError
	require.ErrorAs(t, err, &callError)
	require.Equal(t, "fixture error", callError.Msg)
	output, err := s.Call(t.Context(), "ping")
	require.NoError(t, err)
	require.Equal(t, "pong", output)
	_, err = rpc.New(t.Context(), p)
	require.ErrorIs(t, err, shell.ErrClaimed)
}

func TestConcurrentCallsKeepTheirReplies(t *testing.T) {
	s, _ := session(t)
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := range 12 {
		wg.Go(func() {
			want := fmt.Sprintf("reply-%d", i)
			got, err := s.Call(t.Context(), "eval", "return "+want)
			if err == nil && got != want {
				err = fmt.Errorf("wanted %q, got %q", want, got)
			}
			results <- err
		})
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
}

func TestNoiseIsBoundedAndCopied(t *testing.T) {
	s, _ := session(t)
	result, err := s.Call(t.Context(), "eval", "puts [string repeat x 70000]; return done")
	require.NoError(t, err)
	require.Equal(t, "done", result)
	noise := s.Noise()
	require.Len(t, noise, 64<<10)
	require.True(t, strings.HasSuffix(string(noise), "xxx\n"))
	noise[0] = 'z'
	require.Equal(t, byte('x'), s.Noise()[0])
}

func TestMalformedRepliesBreakSession(t *testing.T) {
	for _, tc := range []struct {
		name, frame, message string
		options              []rpc.Option
		cause                error
	}{
		{name: "header", frame: "TCLRPC1 ok\\n", message: "malformed frame header"},
		{name: "negative length", frame: "TCLRPC1 ok -1\\n", message: "malformed frame length"},
		{name: "nonnumeric length", frame: "TCLRPC1 ok nope\\n", message: "malformed frame length"},
		{name: "unknown status", frame: "TCLRPC1 other 0\\n\\n", message: "unknown frame status"},
		{name: "truncated payload", frame: "TCLRPC1 ok 9\\nabc", message: "short frame"},
		{name: "bad delimiter", frame: "TCLRPC1 ok 2\\nokX", message: "frame delimiter"},
		{name: "frame limit", frame: "TCLRPC1 ok 17\\n", options: []rpc.Option{rpc.WithFrameLimit(16)}, cause: rpc.ErrFrameLimit},
		{name: "line limit", frame: strings.Repeat("x", 33) + "\\n", options: []rpc.Option{rpc.WithLineLimit(32)}, cause: rpc.ErrLineLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, p := session(t, tc.options...)
			_, err := s.Call(t.Context(), "eval", "puts -nonewline stdout \""+tc.frame+"\"; flush stdout; exit 0")
			require.ErrorIs(t, err, rpc.ErrBrokenSession)
			if tc.cause != nil {
				require.ErrorIs(t, err, tc.cause)
			} else {
				require.ErrorContains(t, err, tc.message)
			}
			_, next := s.Call(t.Context(), "ping")
			require.Equal(t, err, next)
			_, done := p.Err()
			require.True(t, done)
		})
	}
}

func TestProcessExitReportsStderr(t *testing.T) {
	s, _ := session(t)
	_, err := s.Call(t.Context(), "eval", "puts stderr {fatal fixture diagnostic}; flush stderr; exit 7")
	require.ErrorIs(t, err, rpc.ErrBrokenSession)
	require.ErrorContains(t, err, "fatal fixture diagnostic")
}

func TestCancellationKillsInFlightSession(t *testing.T) {
	s, p := session(t)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := s.Call(ctx, "eval", "vwait never")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, err, rpc.ErrBrokenSession)
	_, done := p.Err()
	require.True(t, done)
	_, err = s.Call(t.Context(), "ping")
	require.ErrorIs(t, err, rpc.ErrBrokenSession)
}

func TestAlreadyCanceledCallDoesNotSendOrBreakSession(t *testing.T) {
	s, _ := session(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := s.Call(ctx, "eval", "set should_not_exist 1")
	require.ErrorIs(t, err, context.Canceled)
	result, err := s.Call(t.Context(), "eval", "info exists should_not_exist")
	require.NoError(t, err)
	require.Equal(t, "0", result)
}

func TestHandshakeTimeoutKillsProcess(t *testing.T) {
	p := process(t)
	_, err := rpc.New(t.Context(), p, rpc.WithInit("proc ::tclrpc::ping {} {vwait never}"), rpc.WithHandshakeTimeout(50*time.Millisecond))
	require.ErrorIs(t, err, rpc.ErrHandshake)
	require.True(t, errors.Is(err, context.DeadlineExceeded))
	_, done := p.Err()
	require.True(t, done)
}

func TestHandshakeTimeoutIncludesLoadingScripts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	p, err := shell.Start(ctx, "/bin/sh", shell.WithArgs("-c", "exec sleep 10"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	_, err = rpc.New(t.Context(), p, rpc.WithInit(strings.Repeat("x", 1<<20)), rpc.WithHandshakeTimeout(50*time.Millisecond))
	require.ErrorIs(t, err, rpc.ErrHandshake)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, ctx.Err(), "handshake must end before the process lifetime deadline")
	_, done := p.Err()
	require.True(t, done)
}

// Text that is not UTF-8 is refused as a call's error, on either side, and
// the session survives: Tcl 9 refuses to decode invalid UTF-8 or to encode a
// lone surrogate, which ended the loop, and Tcl 8.6 wrote the surrogate as
// invalid UTF-8.
func TestTextThatIsNotUTF8FailsTheCallNotTheSession(t *testing.T) {
	s, _ := session(t, rpc.WithInit("proc echo {value} {return $value}; ::tclrpc::register echo echo"))
	var callError rpc.CallError
	_, err := s.Call(t.Context(), "echo", "caf\xe9")
	require.ErrorAs(t, err, &callError)
	require.Contains(t, callError.Msg, "not valid UTF-8")
	_, err = s.Call(t.Context(), "eval", "format %c 0xD800")
	require.ErrorAs(t, err, &callError)
	require.Contains(t, callError.Msg, "UTF-8")
	output, err := s.Call(t.Context(), "echo", "é")
	require.NoError(t, err)
	require.Equal(t, "é", output)
}

// The loop itself refuses an argument it cannot decode, after reading every
// argument so the stream stays framed, and answers the next call. Go never
// sends one; this writes the frames by hand.
func TestLoopRefusesUndecodableArgumentsAndKeepsServing(t *testing.T) {
	path := os.Getenv("DOCKHAND_TEST_TCLSH")
	if path == "" {
		var err error
		if path, err = exec.LookPath("tclsh"); err != nil {
			t.Skip("tclsh is required for protocol integration tests")
		}
	}
	script := "source loop.tcl\nproc echo {value} {return $value}\n::tclrpc::register echo echo\n::tclrpc::loop\n"
	command := exec.CommandContext(t.Context(), path)
	command.Stdin = strings.NewReader(script + "CALL 2\n4\necho\n4\ncaf\xe9\nCALL 2\n4\necho\n2\nok\n")
	output, err := command.Output()
	require.NoError(t, err)
	replies := string(output)
	probe := exec.CommandContext(t.Context(), path)
	probe.Stdin = strings.NewReader("puts [info tclversion]\n")
	tclVersion, err := probe.Output()
	require.NoError(t, err)
	if strings.HasPrefix(string(tclVersion), "9.") {
		require.Contains(t, replies, "TCLRPC1 err", "Tcl 9 refuses the bytes")
		require.Contains(t, replies, "call argument is not valid UTF-8")
	}
	require.Contains(t, replies, "TCLRPC1 ok 2\nok\n", "the next call is answered")
}
