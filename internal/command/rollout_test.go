package command

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// status, path, archive, and the run commands take -b, -p, and --pr, the
// run commands meaning the branch's latest check, from anywhere (the
// second look's §3).
func TestTheSelectorReachesStatusPathArchiveAndRuns(t *testing.T) {
	w := checkedBranch(t)
	_, _, err := dockhand(t, "check")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", w.clone)

	out, _, err := dockhand(t, "status", "-p", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · ~/Source/macports-branches/jq-update\n", "one branch touches jq: shown in detail")
	out, _, err = dockhand(t, "status", "-b", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "jq-update · ")
	_, _, err = dockhand(t, "status", "jq-update", "-b", "jq-update")
	require.EqualError(t, err, "name the branch once: as the argument, or with -b, -p, or --pr")

	out, _, err = dockhand(t, "path", "-p", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "macports-branches/jq-update\n")

	out, _, err = dockhand(t, "logs", "-p", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "building from ", "jq's log, from its branch's latest check")
	out, _, err = dockhand(t, "logs", "-b", "jq-update")
	require.NoError(t, err)
	require.Contains(t, out, "check-1 · passed\n")
	out, _, err = dockhand(t, "wait", "-p", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "Passed for snapshot 1.\n", "check-1, reported")
	_, _, err = dockhand(t, "wait", "check-1", "-p", "jq")
	require.EqualError(t, err, "name a check or a branch, not both")
	_, _, err = dockhand(t, "cancel", "-b", "jq-update")
	require.EqualError(t, err, "check-1 already passed")

	out, _, err = dockhand(t, "archive", "-p", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "Archived jq-update")
}
