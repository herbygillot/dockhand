package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/stretchr/testify/require"
)

// The operation-shaped reads are bounded and ordered for their operation:
// due jobs soonest first, a contribution's history newest first, open
// contributions by their recorded next look, owed cleanups, and the
// retention sweep's page by ID. The generic query refuses the one
// combination that can skip records, a due ordering paged by ID.
func TestOperationShapedQueries(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	a := repository(t, s, "a")
	one := seed(t, s, a, "one")
	require.NoError(t, s.Update(t.Context(), a.ID, func(ctx context.Context, tx state.Tx) error {
		change, err := tx.Change(ctx, "one")
		if err != nil {
			return err
		}
		change.Branch = "another-name"
		return tx.PutChange(ctx, change)
	}))
	two := seed(t, s, a, "two")
	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, s.Update(t.Context(), a.ID, func(ctx context.Context, tx state.Tx) error {
		for id, retry := range map[record.JobID]time.Time{one.JobID: now.Add(-time.Hour), two.JobID: now.Add(-2 * time.Hour)} {
			job, err := tx.Job(ctx, id)
			if err != nil {
				return err
			}
			at := retry
			job.RetryAt = &at
			if err := tx.PutJob(ctx, job); err != nil {
				return err
			}
		}
		return nil
	}))
	require.NoError(t, s.View(t.Context(), a.ID, func(ctx context.Context, r state.Reader) error {
		due, err := r.DueJobs(ctx, nil, now, 64)
		require.NoError(t, err)
		require.Equal(t, []record.JobID{two.JobID, one.JobID}, []record.JobID{due[0].ID, due[1].ID}, "soonest first")
		due, err = r.DueJobs(ctx, nil, now.Add(-90*time.Minute), 64)
		require.NoError(t, err)
		require.Len(t, due, 1)
		require.Equal(t, two.JobID, due[0].ID)
		due, err = r.DueJobs(ctx, nil, now, 1)
		require.NoError(t, err)
		require.Len(t, due, 1)
		due, err = r.DueJobs(ctx, []record.JobID{}, now, 64)
		require.NoError(t, err)
		require.Empty(t, due, "an empty selection selects nothing")
		_, err = r.DueJobs(ctx, nil, now, 0)
		require.ErrorIs(t, err, state.ErrInvalid)
		_, err = r.Jobs(ctx, state.Query{DueBefore: &now, After: string(two.JobID)})
		require.ErrorIs(t, err, state.ErrInvalid, "a due ordering paged by ID can skip records")
		_, err = r.Resources(ctx, state.Query{DueBefore: &now, After: "r"})
		require.ErrorIs(t, err, state.ErrInvalid)

		history, err := r.JobHistory(ctx, "one")
		require.NoError(t, err)
		require.Len(t, history, 1)
		require.Equal(t, one.JobID, history[0].ID)
		_, err = r.JobHistory(ctx, "")
		require.ErrorIs(t, err, state.ErrInvalid)
		return nil
	}))

	// Open contributions with a pull request, by their next look.
	pr := record.PullRequest{ID: "pr", ChangeID: "two", Ref: record.PullRequestRef{Forge: "fixture", Repository: "macports/macports-ports", Number: 7, URL: "https://example.invalid/pull/7"}, State: record.PullRequestOpen, RemoteHead: record.ObjectID(strings.Repeat("c", 40)), ObservedAt: now}
	require.NoError(t, s.Update(t.Context(), a.ID, func(ctx context.Context, tx state.Tx) error {
		if err := tx.PutPullRequest(ctx, pr); err != nil {
			return err
		}
		change, err := tx.Change(ctx, "two")
		if err != nil {
			return err
		}
		change.PullRequestID = pr.ID
		return tx.PutChange(ctx, change)
	}))
	open := func(due time.Time) []record.ChangeID {
		var ids []record.ChangeID
		require.NoError(t, s.View(t.Context(), a.ID, func(ctx context.Context, r state.Reader) error {
			changes, err := r.OpenContributions(ctx, due, 64)
			for _, change := range changes {
				ids = append(ids, change.ID)
			}
			return err
		}))
		return ids
	}
	require.Equal(t, []record.ChangeID{"two"}, open(now), "a contribution without a PR is not observed; one never looked at is due")
	later := now.Add(time.Hour)
	pr.ObserveAfter = &later
	require.NoError(t, s.Update(t.Context(), a.ID, func(ctx context.Context, tx state.Tx) error { return tx.PutPullRequest(ctx, pr) }))
	require.Empty(t, open(now), "not due yet")
	require.Equal(t, []record.ChangeID{"two"}, open(later), "due at its recorded time")
	require.NoError(t, s.View(t.Context(), a.ID, func(ctx context.Context, r state.Reader) error {
		stored, err := r.PullRequest(ctx, pr.ID)
		require.NoError(t, err)
		require.Equal(t, later, *stored.ObserveAfter, "the schedule round-trips")
		return nil
	}))

	// Owed cleanups: merged with a pending side.
	require.NoError(t, s.Update(t.Context(), a.ID, func(ctx context.Context, tx state.Tx) error {
		change, err := tx.Change(ctx, "one")
		if err != nil {
			return err
		}
		change.Disposition = record.ChangeMerged
		change.Cleanup = &record.BranchCleanup{Local: record.CleanupOutcome{Name: "b", State: record.CleanupComplete}, Fork: record.CleanupOutcome{Name: "o/r:b", State: record.CleanupPending}}
		return tx.PutChange(ctx, change)
	}))
	require.NoError(t, s.View(t.Context(), a.ID, func(ctx context.Context, r state.Reader) error {
		owed, err := r.OwedCleanups(ctx, 64)
		require.NoError(t, err)
		require.Len(t, owed, 1)
		require.Equal(t, record.ChangeID("one"), owed[0].ID)
		return nil
	}))
	require.NoError(t, s.Update(t.Context(), a.ID, func(ctx context.Context, tx state.Tx) error {
		change, err := tx.Change(ctx, "one")
		if err != nil {
			return err
		}
		change.Cleanup.Fork.State = record.CleanupKept
		return tx.PutChange(ctx, change)
	}))
	require.NoError(t, s.View(t.Context(), a.ID, func(ctx context.Context, r state.Reader) error {
		owed, err := r.OwedCleanups(ctx, 64)
		require.NoError(t, err)
		require.Empty(t, owed, "a settled cleanup is not owed")
		return nil
	}))

	// Retention candidates: terminal jobs finished before the cutoff, paged by ID.
	raw, err := sql.Open("sqlite", s.Path())
	require.NoError(t, err)
	defer raw.Close()
	_, err = raw.ExecContext(t.Context(), `UPDATE jobs SET state='completed',finished_at=? WHERE id=?`, now.Add(-time.Hour).UnixMilli(), one.JobID)
	require.NoError(t, err)
	require.NoError(t, s.View(t.Context(), a.ID, func(ctx context.Context, r state.Reader) error {
		jobs, err := r.CleanupCandidates(ctx, now, "", 64)
		require.NoError(t, err)
		require.Len(t, jobs, 1)
		require.Equal(t, one.JobID, jobs[0].ID)
		jobs, err = r.CleanupCandidates(ctx, now, one.JobID, 64)
		require.NoError(t, err)
		require.Empty(t, jobs, "the page after the last ID is empty")
		jobs, err = r.CleanupCandidates(ctx, now.Add(-2*time.Hour), "", 64)
		require.NoError(t, err)
		require.Empty(t, jobs, "a job finished after the cutoff is kept")
		return nil
	}))
}
