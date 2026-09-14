package workflow_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestFilteredStatusKeepsOnlySelectedJobRelations(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		change, err := tx.Change(ctx, "change")
		if err != nil {
			return err
		}
		change.Branch = "candidate"
		return tx.PutChange(ctx, change)
	}))
	finished := completeVerification(t, f, f.request("finished"), record.VerdictFailed).JobID
	waiting := f.submit(t, "waiting")
	f.provider.submit = func(context.Context, verify.Request) (verify.Submission, error) {
		return verify.Submission{State: verify.AtCapacity}, nil
	}
	f.run(t, waiting)
	queued := f.submit(t, "queued")
	standalone := f.request("standalone")
	standalone.Spec.InputRevision = ""
	standalone.Spec.Source = f.source
	standalone.Spec.Checkout = &record.Checkout{Branch: "candidate", Head: f.source.Commit}
	untracked, err := f.engine.Submit(t.Context(), standalone)
	require.NoError(t, err)

	before, err := f.snapshot(t.Context())
	require.NoError(t, err)
	f.engine.Provider = nil
	for _, tc := range []struct {
		name      string
		filter    workflow.StatusFilter
		jobs      []record.JobID
		resources int
	}{
		{"branch", workflow.StatusFilter{Branch: "candidate"}, []record.JobID{finished, waiting, queued}, 1},
		{"branch active", workflow.StatusFilter{Branch: "candidate", Active: true}, []record.JobID{waiting, queued}, 0},
		{"all active", workflow.StatusFilter{Active: true}, []record.JobID{waiting, queued, untracked.JobID}, 0},
		{"finished", workflow.StatusFilter{JobID: finished}, []record.JobID{finished}, 1},
		{"finished active", workflow.StatusFilter{JobID: finished, Active: true}, nil, 0},
		{"missing branch", workflow.StatusFilter{Branch: "missing"}, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, err := f.engine.FilteredStatus(t.Context(), tc.filter)
			require.NoError(t, err)
			require.Equal(t, &tc.filter, status.Filter)
			ids := []record.JobID{}
			for _, job := range status.Jobs {
				ids = append(ids, job.Job.ID)
			}
			require.ElementsMatch(t, tc.jobs, ids)
			require.Len(t, status.Resources, tc.resources)
			if len(tc.jobs) == 0 {
				require.Empty(t, status.Changes)
				require.Empty(t, status.Revisions)
			} else {
				require.Len(t, status.Changes, 1)
				require.Equal(t, "candidate", status.Changes[0].Branch)
				require.Len(t, status.Revisions, 1)
			}
		})
	}
	after, err := f.snapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, before, after, "status must not advance or record work")
	_, err = f.engine.FilteredStatus(t.Context(), workflow.StatusFilter{JobID: "unknown", Active: true})
	require.ErrorIs(t, err, state.ErrNotFound)
}

func TestFilteredStatusPaginationDeduplicatesSharedRecords(t *testing.T) {
	f := newFixture(t)
	for i := range 260 {
		f.submit(t, fmt.Sprintf("job-%03d", i))
	}
	require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
		change, err := tx.Change(ctx, "change")
		if err != nil {
			return err
		}
		change.CurrentRevision = "current"
		change.PullRequestID = "pr"
		if err := tx.PutRevision(ctx, record.Revision{ID: "current", ChangeID: change.ID, Source: f.source, CreatedAt: f.now()}); err != nil {
			return err
		}
		if err := tx.PutPullRequest(ctx, record.PullRequest{ID: "pr", ChangeID: change.ID, Ref: record.PullRequestRef{Forge: "github", Repository: "owner/ports", Number: 1, URL: "https://example.invalid/pull/1"}, RemoteHead: f.source.Commit, ObservedAt: f.now()}); err != nil {
			return err
		}
		if err := tx.PutChange(ctx, change); err != nil {
			return err
		}
		return tx.PutChange(ctx, record.Change{ID: "orphan", Branch: "other", Disposition: record.ChangeOpen})
	}))
	status, err := f.engine.FilteredStatus(t.Context(), workflow.StatusFilter{Active: true})
	require.NoError(t, err)
	require.Len(t, status.Jobs, 260)
	require.Len(t, status.Changes, 1)
	require.Len(t, status.Revisions, 2)
	require.Len(t, status.PullRequests, 1)
	for i := 1; i < len(status.Jobs); i++ {
		require.Less(t, string(status.Jobs[i-1].Job.ID), string(status.Jobs[i].Job.ID))
	}
}

func TestFilteredStatusReuseDoesNotSelectOriginalJobResources(t *testing.T) {
	f := newFixture(t)
	original := completeVerification(t, f, reuseRequest(f, "original"), record.VerdictPassed)
	receipt, err := f.engine.Submit(t.Context(), reuseRequest(f, "reused"))
	require.NoError(t, err)
	f.run(t, receipt.JobID)
	status, err := f.engine.FilteredStatus(t.Context(), workflow.StatusFilter{JobID: receipt.JobID})
	require.NoError(t, err)
	require.Len(t, status.Jobs, 1)
	require.Equal(t, &original, status.Jobs[0].Reused)
	require.Empty(t, status.Jobs[0].Attempts)
	require.Empty(t, status.Resources)
	require.Empty(t, status.Changes)
	require.Empty(t, status.Revisions)
}

type statusWriteStore struct {
	state.Store
	afterSelection func()
	views          int
}
type statusWriteReader struct {
	state.Reader
	store *statusWriteStore
}

func (s *statusWriteStore) View(ctx context.Context, repo record.RepositoryID, fn func(context.Context, state.Reader) error) error {
	s.views++
	return s.Store.View(ctx, repo, func(ctx context.Context, r state.Reader) error {
		return fn(ctx, statusWriteReader{Reader: r, store: s})
	})
}
func (r statusWriteReader) Jobs(ctx context.Context, q state.Query) ([]record.Job, error) {
	jobs, err := r.Reader.Jobs(ctx, q)
	if r.store.afterSelection != nil {
		write := r.store.afterSelection
		r.store.afterSelection = nil
		write()
	}
	return jobs, err
}

func TestFilteredStatusSelectionAndDetailsShareSnapshot(t *testing.T) {
	f := newFixture(t)
	f.submit(t, "queued")
	store := &statusWriteStore{Store: f.store, afterSelection: func() {
		require.NoError(t, f.store.Update(t.Context(), f.repository, func(ctx context.Context, tx state.Tx) error {
			change, err := tx.Change(ctx, "change")
			if err != nil {
				return err
			}
			change.Branch = "changed-during-status"
			return tx.PutChange(ctx, change)
		}))
	}}
	f.engine.State = store
	status, err := f.engine.FilteredStatus(t.Context(), workflow.StatusFilter{Active: true})
	require.NoError(t, err)
	require.Equal(t, 1, store.views)
	require.Len(t, status.Changes, 1)
	require.Empty(t, status.Changes[0].Branch)
	next, err := f.engine.FilteredStatus(t.Context(), workflow.StatusFilter{Active: true})
	require.NoError(t, err)
	require.Equal(t, "changed-during-status", next.Changes[0].Branch)
}
