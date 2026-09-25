package workflow_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/preparation"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestConcurrentPreparationsJoinOneContribution(t *testing.T) {
	t.Parallel()
	f, input := preparationFixture(t, false)
	const count = 8
	receipts := make([]workflow.Receipt, count)
	failures := make([]error, count)
	var group sync.WaitGroup
	for i := range count {
		group.Add(1)
		go func() {
			defer group.Done()
			request := input
			request.ID = record.RequestID(fmt.Sprintf("request-%d", i))
			receipts[i], failures[i] = f.engine.Submit(t.Context(), request)
		}()
	}
	group.Wait()
	for i := range count {
		require.NoError(t, failures[i])
		require.Equal(t, receipts[0].JobID, receipts[i].JobID)
		request := input
		request.ID = record.RequestID(fmt.Sprintf("request-%d", i))
		again, err := f.engine.Submit(t.Context(), request)
		require.NoError(t, err)
		require.Equal(t, receipts[i], again)
	}
	status, err := f.engine.FilteredStatus(t.Context(), workflow.StatusFilter{})
	require.NoError(t, err)
	require.Len(t, status.Jobs, 1)
	job := status.Jobs[0].Job
	require.NotEmpty(t, job.ChangeID)
	require.Nil(t, job.Prepared)
	require.NoError(t, f.store.View(t.Context(), f.repository, func(ctx context.Context, reader state.Reader) error {
		changes, err := reader.Changes(ctx, state.Query{Target: "FIXTURE", Pending: true})
		require.NoError(t, err)
		require.Len(t, changes, 1)
		require.Empty(t, changes[0].Branch)
		require.Empty(t, changes[0].CurrentRevision)
		require.Equal(t, job.ChangeID, changes[0].ID)
		return nil
	}))
}

func TestPreparationStoppedBeforeBranchRetiresAndRetryStartsAfresh(t *testing.T) {
	t.Parallel()
	f, input := preparationFixture(t, false)
	original := f.engine.Preparer
	f.engine.Preparer = prepareFunc(func(context.Context, preparation.Request) (preparation.Result, error) {
		return preparation.Result{}, errors.New("temporary prerequisite missing")
	})
	id := submitPreparation(t, f, input)
	f.run(t, id)
	failed := f.status(t, id)
	require.Equal(t, record.JobNeedsAttention, failed.Jobs[0].Job.State)
	require.Nil(t, failed.Jobs[0].Job.Prepared)
	require.Len(t, failed.Changes, 1)
	require.Equal(t, record.ChangeClosed, failed.Changes[0].Disposition, "a preparation that stops before any branch leaves nothing to pursue")
	require.Empty(t, failed.Changes[0].Branch)
	request := input
	request.ID = "retry"
	request.Spec.Source = commitPort(t, f, "candidate", "version 2\n")
	request.Spec.Source.Base = request.Spec.Source.Commit
	f.engine.Preparer = original
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)
	require.NotEqual(t, id, receipt.JobID)
	retried := f.status(t, receipt.JobID).Jobs[0].Job
	require.NotEqual(t, failed.Jobs[0].Job.ChangeID, retried.ChangeID, "the retry is a new contribution rather than a peer of a retired one")
	require.Equal(t, request.Spec.Source, retried.Spec.Source, "the retry uses the source it was given, not the retired contribution's")
	f.run(t, receipt.JobID)
	f.run(t, receipt.JobID)
	status := f.status(t, receipt.JobID)
	require.Equal(t, record.JobCompleted, status.Jobs[0].Job.State)
	require.Len(t, status.Changes, 1)
	require.Equal(t, retried.ChangeID, status.Changes[0].ID)
	require.Equal(t, record.ChangeOpen, status.Changes[0].Disposition)
	require.NotEmpty(t, status.Changes[0].GeneratedCommit)
	require.Equal(t, record.JobNeedsAttention, f.status(t, id).Jobs[0].Job.State)
}

func TestPreparationDoesNotReplaceDifferentVersionOrOtherContribution(t *testing.T) {
	t.Parallel()
	f, input := preparationFixture(t, false)
	input.Spec.Action = record.Bump
	input.Spec.Version = "2.0"
	id := submitPreparation(t, f, input)
	other := input
	other.ID = "another-version"
	other.Spec.Version = "3.0"
	_, err := f.engine.Submit(t.Context(), other)
	require.ErrorContains(t, err, "another version")
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		return tx.PutChange(ctx, record.Change{ID: "independent", InitiatingTarget: "fixture", Targets: input.Spec.Targets, Branch: "manual", Disposition: record.ChangeOpen, CreatedAt: f.now()})
	}))
	other.Spec.Version = "2.0"
	_, err = f.engine.Submit(t.Context(), other)
	require.ErrorContains(t, err, "multiple open contributions")
	require.Equal(t, record.JobQueued, f.status(t, id).Jobs[0].Job.State)
}
