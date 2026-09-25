package command

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatusFollowsABranchThroughItsWork(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "src", "macports-branches", "jq-update")

	out, _, err := dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "BRANCH     PORTS  WORK         CHECKS  PR\njq-update  0      nothing yet  —       —\n")
	require.Contains(t, out, "serve: not running · queue: empty")

	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "check")
	require.NoError(t, err)

	t.Setenv("MACPORTS_TREE", w.clone)
	out, _, err = dockhand(t)
	require.NoError(t, err, "bare dockhand in a ports checkout is status")
	require.Contains(t, out, "Needs you\n  · jq-update  snapshot 1 passed; commit it for review  dockhand tidy --branch jq-update\n")
	require.Contains(t, out, "jq-update  1      edits, uncommitted  passed for snapshot 1  —\n")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n# more\n"), 0o644))
	out, _, err = dockhand(t, "status", "--attention")
	require.Equal(t, 3, ExitCode(err), "attention needed exits 3")
	require.Equal(t, "  ! jq-update  snapshot 1 passed; the files have changed since  dockhand check --branch jq-update\n", out)

	t.Setenv("MACPORTS_TREE", dir)
	_, _, err = dockhand(t, "tidy")
	require.ErrorContains(t, err, "needs review", "the hand edit is not dockhand's")
	_, _, err = dockhand(t, "tidy", "--squash", "--message", "jq: update to 1.8.1")
	require.NoError(t, err)
	_, _, err = dockhand(t, "check")
	require.NoError(t, err)
	out, _, err = dockhand(t, "status")
	require.NoError(t, err, "inside a worktree, status is the branch's")
	require.Contains(t, out, "jq-update · ~/src/macports-branches/jq-update\n  Ports    jq\n  Work     1 commit above master ")
	require.Contains(t, out, "  Checks   passed for this commit\n           jq  command ✓\n  PR       —\nNext: dockhand submit --branch jq-update\n")

	out, _, err = dockhand(t, "status", "--port", "fd")
	require.NoError(t, err)
	require.Contains(t, out, "No open branches.")
}

func TestQueueWaitCancelAndLogs(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "src", "macports-branches", "jq-update"))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)

	_, _, err = dockhand(t, "check", "-d")
	require.NoError(t, err)
	_, _, err = dockhand(t, "check", "-d")
	require.NoError(t, err)
	out, _, err := dockhand(t, "queue")
	require.NoError(t, err)
	require.Contains(t, out, "serve: not running · queue: 2 runs\n\nRUN      BRANCH     SOURCE      ON       STATE   DETAIL\ncheck-1  jq-update  snapshot 1  command  queued  \ncheck-2")

	out, _, err = dockhand(t, "cancel", "check-2")
	require.NoError(t, err)
	require.Equal(t, "check-2 canceled; finished results are kept.\n", out)
	_, _, err = dockhand(t, "cancel", "check-2")
	require.ErrorContains(t, err, "check-2 already canceled")

	out, errs, err := dockhand(t, "wait", "check-1")
	require.NoError(t, err)
	require.Contains(t, errs, "check-1 runs here, since no dockhand serve is running.")
	require.Contains(t, out, "Passed for snapshot 1.")
	out, _, err = dockhand(t, "wait", "check-1")
	require.NoError(t, err, "a finished run is reported as it ended")
	require.Contains(t, out, "Passed for snapshot 1.")

	out, _, err = dockhand(t, "logs", "check-1")
	require.NoError(t, err)
	require.Contains(t, out, "check-1 · passed: passed\n  command, attempt 1: finished\n    jq passed  ~/.dockhand/logs/check-1/command-1/command.log\n")
	out, _, err = dockhand(t, "logs", "check-1", "--port", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "building from ")
	_, _, err = dockhand(t, "logs", "check-9")
	require.ErrorContains(t, err, "there is no run check-9")
}
