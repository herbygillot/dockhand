package command

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/engine"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// A check made before tidy, the order the design gives, checked the files
// tidy then committed: status credits it to the commit, as submit does.
func TestStatusCreditsACheckOfTheCommittedFilesToTheCommit(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	started, err := jsonOf(t, "start", "jq-update")
	require.NoError(t, err)
	t.Setenv("MACPORTS_TREE", dig(t, started.Result, "branch", "worktree").(string))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)
	_, _, err = dockhand(t, "check")
	require.NoError(t, err)
	_, _, err = dockhand(t, "tidy")
	require.NoError(t, err)

	out, _, err := dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "  Checks   passed for this commit\n")
	require.Contains(t, out, "Next: dockhand submit --branch jq-update\n")

	// Edits on top of the commit are what a check of the working files
	// checked, not the commit.
	dir := dig(t, started.Result, "branch", "worktree").(string)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 1.8.1\n# more\n"), 0o644))
	_, _, err = dockhand(t, "check")
	require.NoError(t, err)
	out, _, err = dockhand(t, "status")
	require.NoError(t, err)
	require.Contains(t, out, "  Checks   passed for snapshot 2\n")
}

func TestStatusFollowsABranchThroughItsWork(t *testing.T) {
	w := newWorld(t)
	versioned(t, w)
	withBumper(t)
	withScript(t, w, "passed")
	_, _, err := dockhand(t, "start", "jq-update")
	require.NoError(t, err)
	dir := filepath.Join(w.home, "Source", "macports-branches", "jq-update")

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
	require.Contains(t, out, "jq-update · ~/Source/macports-branches/jq-update\n  Ports    jq\n  Work     1 commit above master ")
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
	t.Setenv("MACPORTS_TREE", filepath.Join(w.home, "Source", "macports-branches", "jq-update"))
	_, _, err = dockhand(t, "update", "jq")
	require.NoError(t, err)

	_, _, err = dockhand(t, "check", "-d")
	require.NoError(t, err)
	_, _, err = dockhand(t, "check", "-d")
	require.ErrorContains(t, err, "check-1 is already queued for these files")
	out, _, err := dockhand(t, "queue")
	require.NoError(t, err)
	require.Contains(t, out, "serve: not running · queue: 1 run\n\nRUN      BRANCH     SOURCE      ON       STATE   DETAIL\ncheck-1  jq-update  snapshot 1  command  queued  \n")
	out, _, err = dockhand(t, "check", "-d", "--replace")
	require.NoError(t, err)
	require.Contains(t, out, "Stopped check-1; what it finished is kept.\ncheck-2 replaces check-1.\n")

	out, _, err = dockhand(t, "cancel", "check-2")
	require.NoError(t, err)
	require.Equal(t, "check-2 canceled; finished results are kept.\n", out)
	_, _, err = dockhand(t, "cancel", "check-2")
	require.ErrorContains(t, err, "check-2 already canceled")

	_, _, err = dockhand(t, "check", "-d")
	require.NoError(t, err)
	out, errs, err := dockhand(t, "wait", "check-3")
	require.NoError(t, err)
	require.Contains(t, errs, "check-3 runs here, since no dockhand serve is running.")
	require.Contains(t, out, "Passed for snapshot 1.")
	out, _, err = dockhand(t, "wait", "check-3")
	require.NoError(t, err, "a finished run is reported as it ended")
	require.Contains(t, out, "Passed for snapshot 1.")

	out, _, err = dockhand(t, "logs", "check-3")
	require.NoError(t, err)
	require.Regexp(t, `check-3 · passed: passed\n  command, attempt 1, run command_[a-z0-9]{16}: finished\n    jq passed  ~/\.dockhand/logs/check-3/command-1/command\.log\n`, out)
	// A provider run's ID finds its evidence too.
	id := regexp.MustCompile(`command_[a-z0-9]{16}`).FindString(out)
	out, _, err = dockhand(t, "logs", id)
	require.NoError(t, err)
	require.Contains(t, out, "check-3 · passed: passed\n  command, attempt 1, run "+id+": finished\n")
	out, _, err = dockhand(t, "logs", id, "--port", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "building from ")
	_, _, err = dockhand(t, "logs", "tart_nosuchrunatall1")
	require.ErrorContains(t, err, "tart_nosuchrunatall1 is neither a check, such as check-42, nor a provider run")
	out, _, err = dockhand(t, "logs", "check-3", "--port", "jq")
	require.NoError(t, err)
	require.Contains(t, out, "building from ")
	_, _, err = dockhand(t, "logs", "check-9")
	require.ErrorContains(t, err, "there is no run check-9")
}

// A check that passed in an environment made again since says so, and
// asks for another check rather than a submit.
func TestStatusSaysAnEnvironmentWasMadeAgain(t *testing.T) {
	arm := model.Environment{Provider: "tart", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}, DeveloperTools: model.DeveloperToolsXcode}
	latest := model.Run{Number: 3, State: model.RunPassed}
	status := engine.BranchStatus{Branch: model.Branch{Name: "dockhand/jq-update"}, Latest: &latest, Current: true,
		Evidence: &engine.Evidence{Targets: []engine.TargetEvidence{
			{Target: model.PlanTarget{Target: model.Target{Name: "jq"}, Role: model.Changed}, Unchecked: true, Remade: []model.Environment{arm}},
			{Target: model.PlanTarget{Target: model.Target{Name: "oniguruma"}, Role: model.Also}, Unchecked: true, Remade: []model.Environment{arm}},
		}}}
	rows := attentionFor(status)
	require.Len(t, rows, 1)
	require.Equal(t, "check-3 passed, but "+engine.DescribeEnvironment(arm)+" has been made again since, from another source or with other tools; jq must be built there again", rows[0].what)
	require.Equal(t, "dockhand check --branch jq-update", rows[0].next)
}

// A command judges who is alive through one observer session, however
// often it judges: status opened one for its stopped checks and another
// for serve's line on every render, and watch rendered every 30 seconds.
// Each session is a row and two journal events.
func TestStatusOpensOneObserverSession(t *testing.T) {
	w := checkedBranch(t)
	t.Setenv("MACPORTS_TREE", w.clone) // every branch, and serve's line
	started := func() int {
		t.Helper()
		e, err := (&settings{}).open(t.Context())
		require.NoError(t, err)
		defer e.Close()
		var n int
		require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
			n, err = r.CountEvents("session.start", time.Time{})
			return err
		}))
		return n
	}
	before := started()
	_, _, err := dockhand(t, "status")
	require.NoError(t, err)
	require.Equal(t, before+1, started())
}
