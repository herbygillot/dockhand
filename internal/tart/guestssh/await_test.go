package guestssh

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/progress"
)

// answers is a guest that answers each try in turn, as ssh would.
type answers struct {
	tries   int
	replies []reply
}

type reply struct {
	output string
	err    error
}

func (a *answers) Command(context.Context, io.Reader, ...string) ([]byte, error) {
	r := a.replies[min(a.tries, len(a.replies)-1)]
	a.tries++
	return []byte(r.output), r.err
}

// stopped is a VM's run that has ended.
type stopped struct{ err error }

func (s stopped) Done() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}
func (s stopped) Err() error { return s.err }

// A booted guest is waited for while ssh can't connect, and no longer:
// one that answers and refuses ends the wait at once, as does any other
// failure, and the VM's run ending (the code-organization review's
// finding 7).
func TestAGuestIsAwaitedOnlyWhileItCantBeReached(t *testing.T) {
	unreachable := reply{output: "ssh: connect to host 192.168.64.9 port 22: Connection refused", err: ErrTransport}
	guest := &answers{replies: []reply{unreachable, unreachable, {}}}
	require.NoError(t, AwaitSSH(t.Context(), guest, nil, time.Minute, time.Millisecond))
	require.Equal(t, 3, guest.tries, "waited out while it couldn't connect")

	refused := &answers{replies: []reply{{output: "admin@192.168.64.9: Permission denied (publickey,password).", err: ErrTransport}}}
	err := AwaitSSH(t.Context(), refused, nil, time.Minute, time.Millisecond)
	require.ErrorIs(t, err, ErrRefused)
	require.ErrorIs(t, err, ErrTransport)
	require.Equal(t, 1, refused.tries, "a refusal isn't waited out")
	hostKey := &answers{replies: []reply{{output: "Host key verification failed.", err: ErrTransport}}}
	require.ErrorIs(t, AwaitSSH(t.Context(), hostKey, nil, time.Minute, time.Millisecond), ErrRefused)

	other := errors.New("exit status 1")
	require.ErrorIs(t, AwaitSSH(t.Context(), &answers{replies: []reply{{err: other}}}, nil, time.Minute, time.Millisecond), other)

	err = AwaitSSH(t.Context(), &answers{replies: []reply{unreachable}}, stopped{err: errors.New("tart run exited 1")}, time.Minute, time.Hour)
	require.ErrorContains(t, err, "the VM stopped before it accepted SSH: tart run exited 1")
	err = AwaitSSH(t.Context(), &answers{replies: []reply{unreachable}}, nil, 10*time.Millisecond, time.Millisecond)
	require.ErrorContains(t, err, "the guest didn't accept SSH within 10ms")
	require.ErrorIs(t, err, ErrTransport)
}

// The wait says what ssh says, at -v, on its first try, and a wait that
// ends on "No route to host" says what that means: setup -vvvv waited
// four minutes saying nothing, then gave no hint (#89).
func TestTheWaitSaysWhatSSHSays(t *testing.T) {
	noRoute := reply{output: "ssh: connect to host 192.168.64.6 port 22: No route to host", err: ErrTransport}
	var said []progress.Update
	ctx := progress.WithReporter(t.Context(), func(u progress.Update) { said = append(said, u) })
	err := AwaitSSH(ctx, &answers{replies: []reply{noRoute}}, nil, 20*time.Millisecond, time.Millisecond)
	require.ErrorContains(t, err, "the guest didn't accept SSH within 20ms")
	require.ErrorContains(t, err, "SSH reported no route to the guest. If Local Network access is disabled for the app you ran dockhand from")
	require.ErrorContains(t, err, "A VPN or firewall may also block or redirect Tart's 192.168.64.0/24 network")
	require.NotEmpty(t, said)
	require.Equal(t, progress.Verbose, said[0].Level)
	require.Contains(t, said[0].Message, "waiting for SSH on the guest (0s elapsed): ssh: connect to host 192.168.64.6 port 22: No route to host")
	require.Len(t, said, 1, "once a half minute")

	refused := reply{output: "ssh: connect to host 192.168.64.6 port 22: Connection refused", err: ErrTransport}
	err = AwaitSSH(t.Context(), &answers{replies: []reply{refused}}, nil, 10*time.Millisecond, time.Millisecond)
	require.NotContains(t, err.Error(), "no route", "another failure gets no hint")
}

// routed is a guest ssh finds no route to, whose address this Mac routes
// through iface.
type routed struct {
	answers
	iface string
}

func (r *routed) Route(context.Context, string) (string, error) { return r.iface, nil }

// The hint names the app dockhand runs from, by TERM_PROGRAM, for Local
// Network privacy's setting: Ghostty, denied it, held setup's SSH on
// macOS 27.2 (#89).
func TestTheHintNamesTheTerminalApp(t *testing.T) {
	t.Parallel()
	for program, app := range map[string]string{"ghostty": "Ghostty", "Apple_Terminal": "Terminal", "iTerm.app": "iTerm2", "vscode": "Visual Studio Code", "WezTerm": "the app you ran dockhand from", "": "the app you ran dockhand from"} {
		require.Equal(t, app, terminalApp([]string{"HOME=/Users/ada", "TERM_PROGRAM=" + program}), program)
	}
	require.Equal(t, "the app you ran dockhand from", terminalApp(nil))
	hint := noRouteHint(&Guest{Env: []string{"TERM_PROGRAM=ghostty"}}, []byte("ssh: connect to host 192.168.64.6 port 22: No route to host"), ErrTransport)
	require.Contains(t, hint, "If Local Network access is disabled for Ghostty, enable it in System Settings › Privacy & Security › Local Network")
}

// A guest ssh finds no route to, its address routed to Tart's bridge,
// which rules out a VPN, ends the wait after a quarter of it, at most a
// minute, with the hint; one routed elsewhere, as through a VPN's utun, is
// waited out (#89).
func TestANoRouteWaitEndsEarlyOnTartsBridge(t *testing.T) {
	t.Parallel()
	noRoute := reply{output: "ssh: connect to host 192.168.64.6 port 22: No route to host", err: ErrTransport}
	started := time.Now()
	err := AwaitSSH(t.Context(), &routed{answers: answers{replies: []reply{noRoute}}, iface: "bridge100"}, nil, 2*time.Second, time.Millisecond)
	require.ErrorContains(t, err, "the route to its address uses Tart's bridge100")
	require.ErrorContains(t, err, "If Local Network access is disabled")
	require.Less(t, time.Since(started), 2*time.Second, "it ended early")

	started = time.Now()
	err = AwaitSSH(t.Context(), &routed{answers: answers{replies: []reply{noRoute}}, iface: "utun4"}, nil, 400*time.Millisecond, time.Millisecond)
	require.ErrorContains(t, err, "the guest didn't accept SSH within 400ms")
	require.GreaterOrEqual(t, time.Since(started), 400*time.Millisecond, "waited out")
}
