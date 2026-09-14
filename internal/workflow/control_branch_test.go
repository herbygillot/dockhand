package workflow_test

import (
	"context"
	"testing"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
	"github.com/stretchr/testify/require"
)

func trackFixtureBranch(t *testing.T, f *fixture, branch string) {
	t.Helper()
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		change, err := tx.Change(ctx, "change")
		if err != nil {
			return err
		}
		change.Branch = branch
		return tx.PutChange(ctx, change)
	}))
}

func TestBranchScopeFreezesPendingJobs(t *testing.T) {
	f := newFixture(t)
	trackFixtureBranch(t, f, "candidate")
	settled := f.submit(t, "settled")
	f.cancel(t, settled)
	f.run(t, settled)
	first := f.submit(t, "first")

	scope, err := f.engine.BranchScope(t.Context(), "candidate")
	require.NoError(t, err)
	require.Equal(t, []record.JobID{first}, scope.Jobs)

	second := f.submit(t, "second")
	require.Equal(t, []record.JobID{first}, scope.Jobs, "later work must not join an existing attachment")
	current, err := f.engine.BranchScope(t.Context(), "candidate")
	require.NoError(t, err)
	require.ElementsMatch(t, []record.JobID{first, second}, current.Jobs)
}

func TestControlBranchSelectsAndRecordsAtomically(t *testing.T) {
	f := newFixture(t)
	trackFixtureBranch(t, f, "candidate")
	first := f.submit(t, "first")
	second := f.submit(t, "second")
	request := record.ControlRequest{ID: "cancel-branch", Kind: record.Cancel, Reason: "stop this contribution"}

	scope, err := f.engine.ControlBranch(t.Context(), request, "candidate")
	require.NoError(t, err)
	require.ElementsMatch(t, []record.JobID{first, second}, scope.Jobs)

	later := f.submit(t, "later")
	retry, err := f.engine.ControlBranch(t.Context(), request, "candidate")
	require.NoError(t, err)
	require.Equal(t, scope, retry, "an idempotent retry must retain the original selection")

	require.NoError(t, f.store.View(t.Context(), f.repository, func(ctx context.Context, r state.Reader) error {
		control, err := r.Control(ctx, request.ID)
		require.NoError(t, err)
		require.Equal(t, scope.Jobs, control.Jobs)
		return nil
	}))

	f.run(t, scope.Jobs...)
	for _, id := range scope.Jobs {
		require.Equal(t, record.JobCanceled, f.status(t, id).Jobs[0].Job.State)
	}
	require.Equal(t, record.JobQueued, f.status(t, later).Jobs[0].Job.State)
}

func TestBranchSelectionRequiresOpenContributionAndPendingWork(t *testing.T) {
	f := newFixture(t)
	trackFixtureBranch(t, f, "candidate")
	job := f.submit(t, "job")
	f.cancel(t, job)
	f.run(t, job)

	_, err := f.engine.BranchScope(t.Context(), "candidate")
	require.ErrorIs(t, err, workflow.ErrNoPendingJobs)
	_, err = f.engine.ControlBranch(t.Context(), record.ControlRequest{ID: "cancel", Kind: record.Cancel}, "candidate")
	require.ErrorIs(t, err, workflow.ErrNoPendingJobs)
	_, err = f.engine.BranchScope(t.Context(), "missing")
	require.ErrorIs(t, err, state.ErrNotFound)
	_, err = f.engine.BranchScope(t.Context(), "bad..branch")
	require.ErrorIs(t, err, workflow.ErrInvalidRequest)

	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		change, err := tx.Change(ctx, "change")
		if err != nil {
			return err
		}
		change.Disposition = record.ChangeClosed
		return tx.PutChange(ctx, change)
	}))
	_, err = f.engine.BranchScope(t.Context(), "candidate")
	require.ErrorIs(t, err, state.ErrNotFound)
}
