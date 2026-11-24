package engine

import (
	"errors"
	"testing"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/patchcheck"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

func requestOf(t *testing.T, e *Engine, id model.BranchID) *model.ChangeRequest {
	t.Helper()
	return reread(t, e, id).Request
}

// A release is kept on the branch's target the moment it's selected, so
// a preparation that fails after it, as a download does, leaves it for
// the same command to prepare again (#194, H1); the edit written
// concludes the version.
func TestASelectedReleaseIsKeptWhenItsDownloadFails(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, p := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-work"})
	require.NoError(t, err)
	p.refused = errors.New("the download failed")
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.Error(t, err)
	request := requestOf(t, e, branch.ID)
	require.NotNil(t, request)
	require.Len(t, request.Targets, 1)
	require.Equal(t, "jq", request.Targets[0].Port)
	require.NotNil(t, request.Targets[0].Release, "kept before anything was downloaded")
	require.Equal(t, "1.8.1", request.Targets[0].Release.Version)
	open, err := e.UnfinishedTargets(t.Context(), reread(t, e, branch.ID))
	require.NoError(t, err)
	require.Len(t, open, 1, "the same command prepares it again (H3)")

	p.refused = nil
	_, err = e.Update(t.Context(), UpdateRequest{Branch: reread(t, e, branch.ID), Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	outcome, done := requestOf(t, e, branch.ID).Targets[0].Outcome("version")
	require.True(t, done, "the written edit concludes the version")
	require.Equal(t, model.ActionDone, outcome.Result)
	open, err = e.UnfinishedTargets(t.Context(), reread(t, e, branch.ID))
	require.NoError(t, err)
	require.Empty(t, open)
}

// Nothing is committed unattended while a target has no edit: the
// verdict ship asks before its commit says what's owed (#194, C1).
func TestAnUnattendedCommitWaitsForEveryTarget(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-1.8.1", Origin: model.OriginServe})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	require.NoError(t, e.keepTarget(t.Context(), reread(t, e, branch.ID), "serve", "jq-doc", func(target *model.RequestTarget) {
		target.Release = &model.Release{Version: "1.0.2"}
	}))
	prepared, err := e.preparation(t.Context(), reread(t, e, branch.ID), false)
	require.NoError(t, err)
	require.Equal(t, branchstate.UnfinishedPreparation, prepared.Disposition)
	require.True(t, prepared.Pending, "a target owed is more than edits not committed")
	require.Contains(t, prepared.Reasons, "jq-doc has no update yet")
}

// serve's recovery finishes its own preparation that stopped before the
// commit, and queues its check (#194, C3).
func TestRecoveryFinishesAnUnfinishedPreparation(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-stopped", Origin: model.OriginServe})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}})
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	resumed, err := e.ResumePrepared(t.Context(), Where{Environments: []model.Environment{{Provider: "command"}}}, "", true)
	require.NoError(t, err)
	require.Len(t, resumed, 1)
	require.NotNil(t, resumed[0].Run, resumed[0].Problem)
	require.NotEqual(t, string(branch.Base), testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD"), "its edits were committed")
}

// Recovery queues only a branch whose commits are exactly what dockhand
// committed of its edits: a commit on top is left, named (#194, C4, C5).
func TestRecoveryExpectsExactlyWhatDockhandCommitted(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-committed", Origin: model.OriginServe})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: reread(t, e, branch.ID)})
	require.NoError(t, err)
	_, err = e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	testsupport.WriteFiles(t, branch.Worktree, map[string]string{"textproc/jq/Portfile": "name jq\nversion 1.8.1\n# mine\n"})
	testsupport.Git(t, branch.Worktree, "commit", "-qam", "mine on top")
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}})
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	resumed, err := e.ResumePrepared(t.Context(), Where{Environments: []model.Environment{{Provider: "command"}}}, "", true)
	require.NoError(t, err)
	require.Len(t, resumed, 1)
	require.Nil(t, resumed[0].Run)
	require.Contains(t, resumed[0].Problem, "isn't the commit dockhand's tidy made of its edits")
}

// undo restores a tidy and deletes no edit: they're uncommitted again and
// keep their results (#194, X4).
func TestUndoKeepsEdits(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-undone"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: reread(t, e, branch.ID)})
	require.NoError(t, err)
	applied, err := e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	_, _, err = e.Restore(t.Context(), applied.Checkpoint.Name())
	require.NoError(t, err)
	var edits []model.Edit
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		edits, err = r.Edits(branch.ID)
		return err
	}))
	require.Len(t, edits, 1)
	require.NotNil(t, edits[0].Result)
}

// ship's notices are read from the records: a rejected patch that stands
// holds, what the update found and left informs, by port, and a run that
// reads them again, after a restart, lists the same (#194, N1).
func TestShipListsItsUpdatesNoticesFromTheRecords(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, p := f.withPreparer(t)
	p.patches = []patchcheck.Result{{Name: "patch-stale.diff", Checked: true, Detail: "1 out of 1 hunks failed"}}
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-work"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	notices, err := e.Notices(t.Context(), reread(t, e, branch.ID))
	require.NoError(t, err)
	require.Contains(t, notices, Notice{Port: "jq", Words: "patch patch-stale.diff doesn't apply", Holds: true})
	again, err := f.open(t).Notices(t.Context(), reread(t, e, branch.ID))
	require.NoError(t, err)
	require.Equal(t, notices, again, "read again by another process, the same")
}

// Recovery finishes a rename cut short after Git moved the branch before
// it asks Git anything by the branch's name, and queues the check, where
// it skipped the branch as gone (Codex's read of 2c, P2-1).
func TestResumePreparedFinishesRenameAfterGitMove(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-work", Origin: model.OriginServe})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: reread(t, e, branch.ID)})
	require.NoError(t, err)
	_, err = e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	current := intentTo(t, e, reread(t, e, branch.ID), "dockhand/jq-moved")
	require.NoError(t, e.Repo.RenameBranch(t.Context(), current.Name, "dockhand/jq-moved"))
	usePorts(e, fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}})
	e.Providers = map[string]buildenv.Provider{"command": &scriptedProvider{}}
	resumed, err := e.ResumePrepared(t.Context(), Where{Environments: []model.Environment{{Provider: "command"}}}, "", true)
	require.NoError(t, err)
	require.Len(t, resumed, 1, "the branch isn't skipped")
	require.NotNil(t, resumed[0].Run, resumed[0].Problem)
	require.Equal(t, "dockhand/jq-moved", reread(t, e, branch.ID).Name, "the rename was finished first")
}

// A target whose ask changes, a version named in place of another, gets
// a new request ID and loses the outcome its old ask concluded; a first
// selection's save keeps the ID (Codex's read of 2c, P2-2).
func TestChangingANamedVersionClearsItsVersionOutcome(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-work"})
	require.NoError(t, err)
	require.NoError(t, e.keepTarget(t.Context(), branch, "update", "jq", func(target *model.RequestTarget) {}))
	first := requestOf(t, e, branch.ID).ID
	require.NoError(t, e.keepTarget(t.Context(), reread(t, e, branch.ID), "update", "jq", func(target *model.RequestTarget) {
		target.Release = &model.Release{Version: "1.8.1"}
	}))
	require.Equal(t, first, requestOf(t, e, branch.ID).ID, "a first selection's save keeps the ID")
	require.NoError(t, e.keepTarget(t.Context(), reread(t, e, branch.ID), "update", "jq", func(target *model.RequestTarget) {
		concluded(target, model.ActionOutcome{Action: "version", Result: model.ActionDone})
	}))
	require.Equal(t, first, requestOf(t, e, branch.ID).ID, "an outcome keeps the ID")
	require.NoError(t, e.keepTarget(t.Context(), reread(t, e, branch.ID), "update", "jq", func(target *model.RequestTarget) {
		target.Named, target.Release = "1.8.2", &model.Release{Version: "1.8.2"}
	}))
	changed := requestOf(t, e, branch.ID)
	require.NotEqual(t, first, changed.ID, "a version named is a new ask")
	_, done := changed.Targets[0].Outcome("version")
	require.False(t, done, "the old ask's outcome goes with it")
	require.Empty(t, changed.Targets[0].Current)
}

// A target is finished by a written edit at the release kept for it: an
// edit at 1.8.1 doesn't finish a target kept at 1.8.2 (Codex's read of
// 2c, P2-2).
func TestAnEditAtAnotherReleaseDoesntFinishATarget(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-work"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	require.NoError(t, e.keepTarget(t.Context(), reread(t, e, branch.ID), "update", "jq", func(target *model.RequestTarget) {
		target.Named, target.Release = "1.8.2", &model.Release{Version: "1.8.2"}
	}))
	open, err := e.UnfinishedTargets(t.Context(), reread(t, e, branch.ID))
	require.NoError(t, err)
	require.Len(t, open, 1, "the edit at 1.8.1 isn't the 1.8.2 asked")
	prepared, err := e.preparation(t.Context(), reread(t, e, branch.ID), false)
	require.NoError(t, err)
	require.Contains(t, prepared.Reasons, "jq has no update yet")
}
