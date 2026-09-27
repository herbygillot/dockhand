package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// errStopped is a history change stopped at a step, as if its process
// ended there.
var errStopped = errors.New("the process ended here")

func stopAt(step string) func(string) error {
	return func(at string) error {
		if at == step {
			return errStopped
		}
		return nil
	}
}

// updated starts a branch with jq updated and not committed, and plans its
// tidy.
func updated(t *testing.T, e *Engine) (model.Branch, TidyPlan) {
	t.Helper()
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	return branch, plan
}

func checkpointNumbered(t *testing.T, e *Engine, number int) model.Checkpoint {
	t.Helper()
	checkpoint, found := e.history().Read(t.Context(), number)
	require.True(t, found)
	return checkpoint
}

func baseOf(t *testing.T, e *Engine, branch model.Branch) model.ObjectID {
	t.Helper()
	current, err := e.Branch(t.Context(), branch.ID)
	require.NoError(t, err)
	return current.Base
}

// A tidy or rebase that stopped after recording its checkpoint and before
// its Git change leaves a prepared checkpoint, which the next history
// change on the branch abandons, removing what it made. (The architecture
// review of 2026-09-27, finding 3.)
func TestAHistoryChangeStoppedBeforeItsGitChangeIsAbandoned(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, plan := updated(t, e)
	e.stopAt = stopAt("prepared")
	_, err := e.ApplyTidy(t.Context(), plan)
	require.ErrorIs(t, err, errStopped)
	require.Equal(t, string(branch.Base), run(t, branch.Worktree, "rev-parse", "HEAD"), "nothing moved")
	require.Equal(t, model.CheckpointPrepared, checkpointNumbered(t, e, 1).State)

	restarted, _ := f.withPreparer(t)
	result, err := restarted.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, "tidy-2", result.Checkpoint.Name(), "numbers aren't reused")
	require.Equal(t, model.CheckpointAbandoned, checkpointNumbered(t, restarted, 1).State)
	_, _, err = restarted.Restore(t.Context(), "tidy-1")
	require.ErrorContains(t, err, "tidy-1 was never made: dockhand stopped before its change, so there is nothing to restore")

	write(t, f.upstream, map[string]string{"devel/other/Portfile": "name other\n"})
	run(t, f.upstream, "add", "devel/other/Portfile")
	run(t, f.upstream, "commit", "-q", "-m", "other: new port")
	restarted.stopAt = stopAt("prepared")
	_, err = restarted.Rebase(t.Context(), branch)
	require.ErrorIs(t, err, errStopped)
	again, _ := f.withPreparer(t)
	rebased, err := again.Rebase(t.Context(), branch)
	require.NoError(t, err)
	require.Equal(t, "rebase-4", rebased.Checkpoint.Name())
	require.Equal(t, model.CheckpointAbandoned, checkpointNumbered(t, again, 3).State)
	require.Empty(t, run(t, branch.Worktree, "for-each-ref", "refs/dockhand/checkpoints/rebase-3"), "its ref is gone")
}

// A tidy or rebase that stopped after its Git change and before recording
// it leaves a prepared checkpoint, which the next history change on the
// branch records as applied: a rebase's new base with it, and a tidy's
// index reset as the tidy would have reset it. (The architecture review of
// 2026-09-27, finding 3.)
func TestAHistoryChangeStoppedAfterItsGitChangeIsFinished(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, plan := updated(t, e)
	e.stopAt = stopAt("moved")
	result, err := e.ApplyTidy(t.Context(), plan)
	require.ErrorIs(t, err, errStopped)
	require.Equal(t, string(result.Checkpoint.After), run(t, branch.Worktree, "rev-parse", "HEAD"), "the branch moved")
	require.Equal(t, model.CheckpointPrepared, checkpointNumbered(t, e, 1).State, "and the record wasn't finished")

	restarted, _ := f.withPreparer(t)
	_, _, err = restarted.Restore(t.Context(), "tidy-1")
	require.NoError(t, err, "the tidy is recorded first, its index reset, so it can be restored")
	require.Equal(t, model.CheckpointApplied, checkpointNumbered(t, restarted, 1).State)
	require.Equal(t, string(branch.Base), run(t, branch.Worktree, "rev-parse", "HEAD"))

	tidied, err := restarted.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	_, err = restarted.ApplyTidy(t.Context(), tidied)
	require.NoError(t, err)
	oldMaster := baseOf(t, restarted, branch)
	write(t, f.upstream, map[string]string{"devel/other/Portfile": "name other\n"})
	run(t, f.upstream, "add", "devel/other/Portfile")
	run(t, f.upstream, "commit", "-q", "-m", "other: new port")
	restarted.stopAt = stopAt("moved")
	rebased, err := restarted.Rebase(t.Context(), branch)
	require.ErrorIs(t, err, errStopped)
	require.Equal(t, oldMaster, baseOf(t, restarted, branch), "the base wasn't recorded")

	again, _ := f.withPreparer(t)
	up, err := again.Rebase(t.Context(), branch)
	require.NoError(t, err)
	require.True(t, up.UpToDate, "the stopped rebase is recorded, and there is nothing more to do")
	require.Equal(t, model.CheckpointApplied, checkpointNumbered(t, again, rebased.Checkpoint.Number).State)
	require.Equal(t, f.upstreamMaster(t), baseOf(t, again, branch), "with its base")
}

// A restore that stopped after its Git change and before recording it is
// recorded by the next history change on the branch: a rebase's base goes
// back with it. (The architecture review of 2026-09-27, finding 3.)
func TestARestoreStoppedAfterItsGitChangeIsFinished(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch := committedUpdate(t, e)
	oldMaster := baseOf(t, e, branch)
	write(t, f.upstream, map[string]string{"devel/other/Portfile": "name other\n"})
	run(t, f.upstream, "add", "devel/other/Portfile")
	run(t, f.upstream, "commit", "-q", "-m", "other: new port")
	rebased, err := e.Rebase(t.Context(), branch)
	require.NoError(t, err)

	e.stopAt = stopAt("moved")
	_, _, err = e.Restore(t.Context(), rebased.Checkpoint.Name())
	require.ErrorIs(t, err, errStopped)
	require.Equal(t, string(rebased.Checkpoint.Before), run(t, branch.Worktree, "rev-parse", "HEAD"))
	require.Equal(t, f.upstreamMaster(t), baseOf(t, e, branch), "the base wasn't put back")

	restarted, _ := f.withPreparer(t)
	_, _, err = restarted.Restore(t.Context(), rebased.Checkpoint.Name())
	require.ErrorContains(t, err, "was already restored", "the stopped restore was recorded first")
	require.NotNil(t, checkpointNumbered(t, restarted, rebased.Checkpoint.Number).RestoredAt)
	require.Equal(t, oldMaster, baseOf(t, restarted, branch), "with the base it started from")
}

// uncertainStore reports one transaction's commit as uncertain: after it
// committed, when landed, or after rolling it back.
type uncertainStore struct {
	store.Store
	when   func(*watchedTx) bool
	landed bool
	fired  bool
}

// watchedTx notes what a transaction recorded about checkpoints.
type watchedTx struct {
	store.Tx
	added   bool
	settled model.CheckpointState
}

func (t *watchedTx) AddCheckpoint(c model.Checkpoint) error {
	t.added = true
	return t.Tx.AddCheckpoint(c)
}

func (t *watchedTx) SettleCheckpoint(c model.Checkpoint) error {
	t.settled = c.State
	return t.Tx.SettleCheckpoint(c)
}

var errRolledBack = errors.New("rolled back")

func (s *uncertainStore) Update(ctx context.Context, repository model.RepositoryID, fn func(store.Tx) error) error {
	var watched *watchedTx
	err := s.Store.Update(ctx, repository, func(tx store.Tx) error {
		watched = &watchedTx{Tx: tx}
		if err := fn(watched); err != nil {
			return err
		}
		if !s.fired && s.when(watched) && !s.landed {
			return errRolledBack
		}
		return nil
	})
	if watched != nil && !s.fired && s.when(watched) {
		s.fired = true
		if err == nil || errors.Is(err, errRolledBack) {
			return store.ErrUncertain
		}
	}
	return err
}

// A commit the store reports as uncertain is read back before anything
// more is done: one that landed carries on, and one that didn't stops
// the change where it is. A Git change once made is never undone. (The
// architecture review of 2026-09-27, finding 3.)
func TestAnUncertainCommitIsReadBack(t *testing.T) {
	prepared := func(tx *watchedTx) bool { return tx.added }
	applied := func(tx *watchedTx) bool { return tx.settled == model.CheckpointApplied }
	for _, c := range []struct {
		name   string
		when   func(*watchedTx) bool
		landed bool
		check  func(t *testing.T, e *Engine, branch model.Branch, result TidyResult, err error)
	}{
		{"the checkpoint landed", prepared, true, func(t *testing.T, e *Engine, branch model.Branch, result TidyResult, err error) {
			require.NoError(t, err)
			require.Equal(t, model.CheckpointApplied, checkpointNumbered(t, e, 1).State)
		}},
		{"the checkpoint didn't land", prepared, false, func(t *testing.T, e *Engine, branch model.Branch, result TidyResult, err error) {
			require.ErrorContains(t, err, "recording the checkpoint failed, so nothing was changed")
			require.Equal(t, string(branch.Base), run(t, branch.Worktree, "rev-parse", "HEAD"))
			_, found := e.history().Read(t.Context(), 1)
			require.False(t, found)
		}},
		{"its settling landed", applied, true, func(t *testing.T, e *Engine, branch model.Branch, result TidyResult, err error) {
			require.NoError(t, err)
			require.Equal(t, model.CheckpointApplied, checkpointNumbered(t, e, 1).State)
		}},
		{"its settling didn't land", applied, false, func(t *testing.T, e *Engine, branch model.Branch, result TidyResult, err error) {
			require.ErrorContains(t, err, "the commits are made, but recording it failed")
			require.ErrorContains(t, err, "the next dockhand tidy, rebase, or restore of this branch finishes the record")
			require.Equal(t, string(result.Checkpoint.After), run(t, branch.Worktree, "rev-parse", "HEAD"), "never undone")
			require.Equal(t, model.CheckpointPrepared, checkpointNumbered(t, e, 1).State)
			_, _, err = e.Restore(t.Context(), "tidy-1")
			require.NoError(t, err, "the next history change records it, and it can be restored")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := setup(t)
			e, _ := f.withPreparer(t)
			branch, plan := updated(t, e)
			e.Store = &uncertainStore{Store: e.Store, when: c.when, landed: c.landed}
			result, err := e.ApplyTidy(t.Context(), plan)
			c.check(t, e, branch, result, err)
		})
	}
}

// A history change waits for the branch's lock, which another one holds
// throughout its change, and changes nothing if it gives up waiting.
func TestAHistoryChangeWaitsForTheBranchsLock(t *testing.T) {
	f := setup(t)
	e, _ := f.withPreparer(t)
	branch, plan := updated(t, e)
	held, release, done := make(chan struct{}), make(chan struct{}), make(chan error)
	go func() {
		done <- e.Repo.WithBranchLock(context.Background(), branch.Name, func(context.Context) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	waiting, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	_, err := e.ApplyTidy(waiting, plan)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, string(branch.Base), run(t, branch.Worktree, "rev-parse", "HEAD"))
	_, found := e.history().Read(t.Context(), 1)
	require.False(t, found)
	close(release)
	require.NoError(t, <-done)
	_, err = e.ApplyTidy(t.Context(), plan)
	require.NoError(t, err)
}
