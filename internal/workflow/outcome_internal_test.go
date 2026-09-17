package workflow

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOutcomeKindsAndReporting(t *testing.T) {
	t.Parallel()
	require.Equal(t, waiting, waitingFor(waitBuild, "progress").kind)
	require.Equal(t, waitBuild, waitingFor(waitBuild, "progress").wait)
	require.Empty(t, waitingFor(waitBuild, "progress").problem(), "waiting is not a cycle problem")
	err := errors.New("boom")
	require.Equal(t, failed, failure(err).kind)
	require.Equal(t, "boom", failure(err).problem())
	require.ErrorIs(t, failure(err).err, err)
	require.Equal(t, waiting, failure(nil).kind, "no error is not a failure")
	require.Equal(t, failed, failuref("bad %d", 1).kind)
	require.Equal(t, "bad 1", failuref("bad %d", 1).detail)
	require.Equal(t, settled, settledWith("done").kind)
	require.Empty(t, settledWith("done").problem())
	require.Equal(t, "rejected", settledProblem("rejected").problem())
}
