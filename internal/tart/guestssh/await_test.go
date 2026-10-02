package guestssh

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
