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
	require.ErrorContains(t, err, "this Mac has no route to the guest's address: a VPN or firewall that routes Tart's 192.168.64.0/24 network elsewhere does this")
	require.NotEmpty(t, said)
	require.Equal(t, progress.Verbose, said[0].Level)
	require.Contains(t, said[0].Message, "waiting for the guest's SSH (0s so far): ssh: connect to host 192.168.64.6 port 22: No route to host")
	require.Len(t, said, 1, "once a half minute")

	refused := reply{output: "ssh: connect to host 192.168.64.6 port 22: Connection refused", err: ErrTransport}
	err = AwaitSSH(t.Context(), &answers{replies: []reply{refused}}, nil, 10*time.Millisecond, time.Millisecond)
	require.NotContains(t, err.Error(), "no route", "another failure gets no hint")
}
