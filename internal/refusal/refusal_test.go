package refusal

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// A refusal is reached through wrapping, by its kind, never its words; its
// kind decides its exit code and whether another try may meet it.
func TestARefusalIsReachedByItsKind(t *testing.T) {
	cause := errors.New("dial tcp: connection reset by peer")
	r := Wrap(Network, cause, "couldn't reach GitHub (%v)", cause).With("nothing was changed")
	wrapped := fmt.Errorf("fetching master: %w", r)
	require.Equal(t, Network, KindOf(wrapped))
	require.True(t, Transient(wrapped))
	require.ErrorIs(t, wrapped, cause)
	require.Equal(t, "couldn't reach GitHub (dial tcp: connection reset by peer)\nKept: nothing was changed.", r.Error())
	require.Equal(t, Failed, KindOf(errors.New("plain")))
	require.False(t, Transient(errors.New("plain")))
	require.Equal(t, 3, Attention.Exit())
	require.Equal(t, 2, CheckFailed.Exit())
	require.Equal(t, 1, Auth.Exit())
	require.Panics(t, func() { New("", "no kind") })
	require.True(t, Transient(errors.Join(errors.New("a"), New(UpstreamNotYet, "mirrors behind"))))
}
