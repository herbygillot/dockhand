package engine

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Your checkout's master is never one of dockhand's branches: a command
// run there names how to pick one, not adopt (the rc5 full stage, A4).
func TestMasterIsYourCheckoutsNotABranch(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	_, err := e.Current(t.Context())
	require.ErrorIs(t, err, ErrNoBranch)
	require.ErrorIs(t, err, ErrYourCheckout)
	require.ErrorContains(t, err, "this is your checkout's master, not one of dockhand's branches; name one with -b <branch>")
	require.NotContains(t, err.Error(), "adopt")
}
