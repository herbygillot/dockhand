package rpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/tcl/shell"
	"github.com/herbygillot/dockhand/internal/testenv"
)

func startShell(t *testing.T) *shell.Proc {
	t.Helper()
	path := testenv.Tclsh(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p, err := shell.Start(ctx, path)
	require.NoError(t, err)
	return p
}

func startSession(t *testing.T, initScripts ...string) *Session {
	t.Helper()
	return startSessionWith(t, nil, initScripts...)
}

// startSessionWith is startSession for the tests that need a session built
// with non-default transport bounds.
func startSessionWith(t *testing.T, opts []Option, initScripts ...string) *Session {
	t.Helper()
	p := startShell(t)
	s, err := New(context.Background(), p, append([]Option{WithInit(initScripts...)}, opts...)...)
	require.NoError(t, err) // New kills the proc on failure
	t.Cleanup(func() { s.Close() })
	return s
}

func call(t *testing.T, s *Session, op string, args ...string) string {
	t.Helper()
	got, err := s.Call(context.Background(), op, args...)
	require.NoError(t, err, "Call(%s)", op)
	return got
}

func TestSessionPing(t *testing.T) {
	s := startSession(t)
	require.Equal(t, "pong", call(t, s, "ping"))
}

func TestSessionEval(t *testing.T) {
	s := startSession(t)
	require.Equal(t, "42", call(t, s, "eval", "expr {6 * 7}"))
	require.Equal(t, "b c", call(t, s, "eval", `lindex {a {b c} d} 1`))
}

func TestSessionHandlerErrorKeepsSessionAlive(t *testing.T) {
	s := startSession(t)
	_, err := s.Call(context.Background(), "eval", "error boom")
	var ce CallError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, "boom", ce.Msg)
	require.Equal(t, "pong", call(t, s, "ping"), "session must survive a handler error")
}

func TestSessionUnknownOp(t *testing.T) {
	s := startSession(t)
	_, err := s.Call(context.Background(), "no-such-op")
	var ce CallError
	require.ErrorAs(t, err, &ce)
	require.Contains(t, ce.Msg, "unknown op")
}

func TestSessionNoiseTolerance(t *testing.T) {
	s := startSession(t, `
proc noisy {} {
    puts "stray line 1"
    puts "DOCKHAND-lookalike but not a frame"
    return clean
}
::tclrpc::register noisy noisy
`)
	require.Equal(t, "clean", call(t, s, "noisy"))
	require.Contains(t, string(s.Noise()), "stray line 1")
}

func TestSessionBinarySafePayloads(t *testing.T) {
	s := startSession(t)
	// Newlines, braces, quotes, dollars, unicode — nothing on the wire is
	// quoted, so nothing should need care.
	arg := "line1\nline2 {brace} \"quote\" $dollar café"
	require.Contains(t, call(t, s, "eval", "string toupper {"+arg+"}"), "CAFÉ")
	require.Len(t, call(t, s, "eval", "string repeat x 1000000"), 1000000)
}

func TestSessionRoundTripThroughArgs(t *testing.T) {
	s := startSession(t, `
proc echo2 {a b} { return "$a|$b" }
::tclrpc::register echo2 echo2
`)
	require.Equal(t, "with\nnewline|with}brace", call(t, s, "echo2", "with\nnewline", "with}brace"))
}

func TestSessionTimeoutBreaksSession(t *testing.T) {
	s := startSession(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := s.Call(ctx, "eval", "after 10000")
	require.Error(t, err, "hung call must not return")
	// The call that witnessed the break branches on the same sentinel as
	// the ones after it, and the cause stays reachable through it.
	require.ErrorIs(t, err, ErrBrokenSession)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	_, err = s.Call(context.Background(), "ping")
	require.ErrorIs(t, err, ErrBrokenSession)
}

func TestSessionFrameLimitBreaksSession(t *testing.T) {
	s := startSessionWith(t, []Option{WithFrameLimit(4 << 10)})
	_, err := s.Call(context.Background(), "eval", "string repeat x 20000")
	require.ErrorIs(t, err, ErrFrameLimit)
	require.ErrorIs(t, err, ErrBrokenSession)
	// A handler error leaves a session usable; a frame past the limit is
	// a protocol violation and must not be mistaken for one.
	var ce CallError
	require.NotErrorAs(t, err, &ce, "a limit breach is not a handler error")
	requireBrokenAndSilent(t, s)
}

func TestSessionFrameLimitIsCheckedBeforeAllocation(t *testing.T) {
	// A child that advertises a length it has no intention of writing is
	// the whole reason the check precedes the make(): the frame below
	// claims a terabyte and sends four bytes, and the session must refuse
	// it without waiting for, or allocating for, the rest.
	s := startSessionWith(t, []Option{WithFrameLimit(4 << 10)}, `
proc liar {} {
    puts stdout "TCLRPC1 ok 1099511627776"
    puts stdout "nope"
    flush stdout
    return unreached
}
::tclrpc::register liar liar
`)
	done := make(chan error, 1)
	go func() {
		_, err := s.Call(context.Background(), "liar")
		done <- err
	}()
	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrFrameLimit)
		require.ErrorIs(t, err, ErrBrokenSession)
	case <-time.After(30 * time.Second):
		t.Fatal("session waited on a frame it should have refused outright")
	}
	requireBrokenAndSilent(t, s)
}

func TestSessionLineLimitBreaksSession(t *testing.T) {
	// Noise, not a frame: the tolerance for stray stdout is what makes an
	// unbounded line read reachable, so the bound has to hold there too.
	s := startSessionWith(t, []Option{WithLineLimit(4 << 10)}, `
proc shouty {} {
    puts [string repeat x 20000]
    return quiet
}
::tclrpc::register shouty shouty
`)
	_, err := s.Call(context.Background(), "shouty")
	require.ErrorIs(t, err, ErrLineLimit)
	require.ErrorIs(t, err, ErrBrokenSession)
	requireBrokenAndSilent(t, s)
}

func TestSessionDefaultLimitsAdmitRealPayloads(t *testing.T) {
	// The bounds are sized above what MacPorts actually returns; the
	// largest Portfile in the ports tree is 139 KiB, and a snapshot of it
	// is smaller than the file. A megabyte with room to spare is the
	// contract.
	s := startSession(t)
	require.Len(t, call(t, s, "eval", "string repeat x 1000000"), 1000000)
	require.Equal(t, "pong", call(t, s, "ping"), "a legitimate payload must not break the session")
}

// requireBrokenAndSilent asserts the sticky half of the contract: a broken
// session answers every later call with ErrBrokenSession, and answers it
// from its own recorded state rather than by touching a dead process.
func requireBrokenAndSilent(t *testing.T, s *Session) {
	t.Helper()
	select {
	case <-s.proc.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("a broken session left its process running")
	}
	for i := range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := s.Call(ctx, "ping")
		cancel()
		require.ErrorIs(t, err, ErrBrokenSession, "call %d after breakage", i)
		require.NotErrorIs(t, err, context.DeadlineExceeded,
			"a broken session must answer from its own state, not by waiting on I/O")
	}
}

func TestSessionRefusesClaimedProc(t *testing.T) {
	p := startShell(t)
	t.Cleanup(p.Kill)
	require.NoError(t, p.Claim())
	_, err := New(context.Background(), p)
	require.ErrorIs(t, err, shell.ErrClaimed)
	select {
	case <-p.Done():
		t.Fatal("refused New killed another owner's proc")
	default:
	}
}

func TestSessionHandshakeTimeout(t *testing.T) {
	p := startShell(t)
	// Violate freshness: something else has written to stdin, wedging the
	// command stream before the loop can load.
	_, err := p.Stdin().Write([]byte("exec sleep 60\n"))
	require.NoError(t, err)
	_, err = New(context.Background(), p, WithHandshakeTimeout(300*time.Millisecond))
	require.ErrorIs(t, err, ErrHandshake)
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("failed handshake did not kill the proc")
	}
}
