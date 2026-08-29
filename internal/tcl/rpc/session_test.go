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
	p := startShell(t)
	s, err := New(context.Background(), p, WithInit(initScripts...))
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
	_, err = s.Call(context.Background(), "ping")
	require.ErrorIs(t, err, ErrBroken)
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
