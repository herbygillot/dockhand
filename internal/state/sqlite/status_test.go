package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestJobStatusFiltersBeforeDecodingAndWithinRepository(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	a, b := repository(t, s, "a"), repository(t, s, "b")
	first, foreign := seed(t, s, a, "first"), seed(t, s, b, "foreign")
	e := workflow.Engine{State: s, Repository: a.ID}
	bad, err := e.Submit(t.Context(), workflow.Request{ID: "unrelated", Spec: record.JobSpec{
		Action: record.Verify, Source: source(), Targets: []record.Target{{Name: "fixture", Portfile: "Portfile"}},
		Destination: record.VerificationComplete, Verification: record.VerificationRequired,
	}})
	require.NoError(t, err)
	raw, err := sql.Open("sqlite", s.Path())
	require.NoError(t, err)
	defer raw.Close()
	_, err = raw.ExecContext(t.Context(), `UPDATE jobs SET state='completed',options='{"Build":42}' WHERE id=?`, bad.JobID)
	require.NoError(t, err)
	require.NoError(t, s.Update(t.Context(), a.ID, func(ctx context.Context, tx state.Tx) error {
		job, err := tx.Job(ctx, first.JobID)
		if err != nil {
			return err
		}
		later := time.Now().Add(24 * time.Hour)
		job.RetryAt = &later
		return tx.PutJob(ctx, job)
	}))
	require.NoError(t, s.View(t.Context(), a.ID, func(ctx context.Context, r state.Reader) error {
		for _, q := range []state.Query{{Pending: true}, {Branch: "shared-name"}, {Jobs: []record.JobID{first.JobID}}} {
			jobs, err := r.Jobs(ctx, q)
			require.NoError(t, err)
			require.Len(t, jobs, 1)
			require.Equal(t, first.JobID, jobs[0].ID)
		}
		for _, q := range []state.Query{{Jobs: []record.JobID{foreign.JobID}}, {Branch: "' OR 1=1 --"}, {Pending: true, After: string(first.JobID)}} {
			jobs, err := r.Jobs(ctx, q)
			require.NoError(t, err)
			require.Empty(t, jobs)
		}
		_, err := r.Jobs(ctx, state.Query{})
		require.Error(t, err, "the excluded row must actually be undecodable")
		return nil
	}))
	require.NoError(t, s.Update(t.Context(), a.ID, func(ctx context.Context, tx state.Tx) error {
		change, err := tx.Change(ctx, "first")
		if err != nil {
			return err
		}
		change.Disposition = record.ChangeClosed
		return tx.PutChange(ctx, change)
	}))
	closed, err := e.FilteredStatus(t.Context(), workflow.StatusFilter{Branch: "shared-name", Active: true})
	require.NoError(t, err)
	require.Len(t, closed.Jobs, 1)
	require.Equal(t, record.ChangeClosed, closed.Changes[0].Disposition)
	_, err = e.FilteredStatus(t.Context(), workflow.StatusFilter{JobID: foreign.JobID})
	require.ErrorIs(t, err, state.ErrNotFound)
}
