package engine

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
)

func TestEditExpandsTheWorktreeToThePort(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "notes"})
	require.NoError(t, err)
	directory, err := e.Edit(t.Context(), branch, "jq")
	require.NoError(t, err)
	require.Equal(t, "textproc/jq", directory)
	require.FileExists(t, filepath.Join(branch.Worktree, "textproc/jq/Portfile"))
	e.PortReader = fakePorts{}
	_, err = e.Edit(t.Context(), branch, "nosuchport")
	require.ErrorContains(t, err, "no port nosuchport in this tree")
}

func TestRevbumpRecordsItsReasonForTidy(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "poppler-25.09"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditRevbump, Port: "jq"})
	require.ErrorContains(t, err, "needs its reason as the subject")
	update, err := e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditRevbump, Port: "jq", Subject: "rebuild for oniguruma 6.9.10"})
	require.NoError(t, err)
	require.Equal(t, 0, update.Before.Revision)
	require.Equal(t, 1, update.After.Revision)
	require.Equal(t, "name jq\nversion 1.7.1\nrevision 1\n", read(t, filepath.Join(branch.Worktree, "textproc/jq/Portfile")))

	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.True(t, plan.Unambiguous())
	require.Equal(t, "jq: rebuild for oniguruma 6.9.10", plan.Groups[0].Subject())
}

func TestRebaseMovesTheBranchOntoFreshMaster(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update", Here: true})
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	commitAs(t, branch.Worktree, "Ada ada@example.org", "jq: update to 1.8.1")

	up, err := e.Rebase(t.Context(), branch)
	require.NoError(t, err)
	require.True(t, up.UpToDate, "nothing new on master")

	write(t, f.upstream, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	run(t, f.upstream, "commit", "-q", "-am", "libharbor: update to 3")
	rebased, err := e.Rebase(t.Context(), branch)
	require.NoError(t, err)
	require.False(t, rebased.UpToDate)
	require.Equal(t, f.upstreamMaster(t), rebased.To)
	require.Equal(t, "rebase-1", rebased.Checkpoint.Name())
	require.Equal(t, []string{"jq: update to 1.8.1"}, log(t, branch.Worktree, rebased.To))
	current, err := e.Resolve(t.Context(), "jq-update")
	require.NoError(t, err)
	require.Equal(t, rebased.To, current.Base, "the branch's base moves with it")

	require.Equal(t, rebased.From, rebased.Checkpoint.BaseBefore)
	require.Equal(t, rebased.To, rebased.Checkpoint.BaseAfter)

	// Restoring puts back the history, the files, and the base together.
	// (The architecture review of 2026-09-27, finding 3.)
	write(t, branch.Worktree, map[string]string{"devel/libharbor/Portfile": "mine\n"})
	_, _, err = e.Restore(t.Context(), "rebase-1")
	require.ErrorContains(t, err, "restoring rebase-1 puts back master's older files too, and a change to one of them stops it, so nothing was changed")
	require.Equal(t, string(rebased.Checkpoint.After), run(t, branch.Worktree, "rev-parse", "HEAD"))
	run(t, branch.Worktree, "checkout", "--", "devel/libharbor/Portfile")
	_, restored, err := e.Restore(t.Context(), "rebase-1")
	require.NoError(t, err)
	require.Equal(t, string(rebased.Checkpoint.Before), run(t, branch.Worktree, "rev-parse", "HEAD"))
	require.Empty(t, run(t, branch.Worktree, "status", "--porcelain"), "master's newer files don't read as the branch's edits")
	require.Equal(t, rebased.From, restored.Base)
	current, err = e.Resolve(t.Context(), "jq-update")
	require.NoError(t, err)
	require.Equal(t, rebased.From, current.Base, "the history put back starts from the master it started from")
	_, err = e.PlanTidy(t.Context(), TidyRequest{Branch: current})
	require.NoError(t, err, "not: is not above its base")
}

func TestARebaseThatConflictsChangesNothing(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update", Here: true})
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	commitAs(t, branch.Worktree, "Ada ada@example.org", "jq: update to 1.8.1")
	head := run(t, branch.Worktree, "rev-parse", "HEAD")
	write(t, f.upstream, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.0\n"})
	run(t, f.upstream, "commit", "-q", "-am", "jq: update to 1.8.0")

	_, err = e.Rebase(t.Context(), branch)
	require.ErrorContains(t, err, "the rebase stopped on conflicts in textproc/jq/Portfile")
	require.Equal(t, head, run(t, branch.Worktree, "rev-parse", "HEAD"))
	require.Empty(t, run(t, branch.Worktree, "status", "--porcelain"))

	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "mine\n"})
	_, err = e.Rebase(t.Context(), branch)
	require.ErrorContains(t, err, "has uncommitted edits")
}

func TestRetryAndArchive(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	queued := queuedHarborRun(t, e, tahoeArm)
	_, err := e.Retry(t.Context(), queued)
	require.ErrorContains(t, err, "is still queued")
	finished, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	again, err := e.Retry(t.Context(), finished)
	require.NoError(t, err)
	require.Equal(t, finished.Plan, again.Plan)
	require.Equal(t, finished.Revision, again.Revision, "the pinned request, whatever the branch holds now")
	require.Equal(t, model.RunQueued, again.State)

	branch, err := e.Branch(t.Context(), finished.Branch)
	require.NoError(t, err)
	archived, err := e.Archive(t.Context(), branch, false)
	require.NoError(t, err)
	require.Equal(t, model.BranchArchived, archived.State)
	open, err := e.Status(t.Context())
	require.NoError(t, err)
	require.Empty(t, open)
	back, err := e.Archive(t.Context(), branch, true)
	require.NoError(t, err)
	require.Equal(t, model.BranchOpen, back.State)
}

// A rebase counts the commits it replays: a change master already has is
// dropped, and not counted. A branch already on master is changed in
// nothing, and no rebase is recorded for it.
func TestARebaseCountsWhatItReplays(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update", Here: true})
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n"})
	commitAs(t, branch.Worktree, "Ada ada@example.org", "jq: update to 1.8.1")
	write(t, branch.Worktree, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	commitAs(t, branch.Worktree, "Ada ada@example.org", "libharbor: update to 3")

	rebases := func() int {
		t.Helper()
		events, err := e.Events(t.Context(), 0)
		require.NoError(t, err)
		return len(slices.DeleteFunc(events, func(event model.Event) bool { return event.Kind != "branch.rebase" }))
	}
	up, err := e.Rebase(t.Context(), branch)
	require.NoError(t, err)
	require.True(t, up.UpToDate)
	require.Zero(t, rebases(), "a branch already on master records no rebase from master onto itself")

	write(t, f.upstream, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	run(t, f.upstream, "commit", "-q", "-am", "libharbor: update to 3")
	rebased, err := e.Rebase(t.Context(), branch)
	require.NoError(t, err)
	require.Equal(t, 1, rebased.Commits, "libharbor's change is master's now")
	require.Equal(t, []string{"jq: update to 1.8.1"}, log(t, branch.Worktree, rebased.To))
	require.Equal(t, 1, rebases())
}
