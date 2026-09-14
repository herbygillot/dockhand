package sqlite_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/stretchr/testify/require"
)

func TestVerificationLookupIsBoundedScopedAndIncludesNegativeEvidence(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	a, b := repository(t, s, "a"), repository(t, s, "b")
	one, two := seed(t, s, a, "a"), seed(t, s, b, "b")
	for _, input := range []struct {
		repo   record.Repository
		job    record.JobID
		prefix string
	}{{a, one.JobID, "a"}, {b, two.JobID, "b"}} {
		require.NoError(t, s.Update(t.Context(), input.repo.ID, func(ctx context.Context, tx state.Tx) error {
			job, err := tx.Job(ctx, input.job)
			if err != nil {
				return err
			}
			for i := range 40 {
				verdict := record.VerdictPassed
				if i == 39 {
					verdict = record.VerdictFailed
				}
				attempt := record.Attempt{ID: record.AttemptID(fmt.Sprintf("%s-%02d", input.prefix, i)), JobID: job.ID, TargetID: "target", Spec: record.BuildSpec{RevisionID: job.Spec.InputRevision, Source: job.Spec.Source, Target: job.Spec.Targets[0], Config: *job.Spec.Build}, State: record.AttemptFinished, CreatedAt: job.AcceptedAt.Add(time.Duration(i) * time.Millisecond), Evidence: &record.Evidence{Verdict: verdict, ObservedAt: job.AcceptedAt.Add(time.Duration(i+1) * time.Millisecond)}}
				if err := tx.PutAttempt(ctx, attempt); err != nil {
					return err
				}
			}
			return nil
		}))
	}
	require.NoError(t, s.View(t.Context(), a.ID, func(ctx context.Context, r state.Reader) error {
		query := state.VerificationQuery{Target: record.Target{Name: "fixture", Portfile: "Portfile"}, Tree: source().Tree, Limit: 32}
		attempts, err := r.VerificationCandidates(ctx, query)
		require.NoError(t, err)
		require.Len(t, attempts, 32)
		require.Equal(t, record.AttemptID("a-39"), attempts[0].ID)
		require.Equal(t, record.VerdictFailed, attempts[0].Evidence.Verdict)
		require.Equal(t, record.AttemptID("a-08"), attempts[31].ID)
		query.Target.Name = ""
		query.Limit = 1
		attempts, err = r.VerificationCandidates(ctx, query)
		require.NoError(t, err)
		require.Len(t, attempts, 1)
		require.Equal(t, record.AttemptID("a-39"), attempts[0].ID)
		require.Equal(t, "fixture", attempts[0].Spec.Target.Name)
		query.Target.Portfile = "other/Portfile"
		attempts, err = r.VerificationCandidates(ctx, query)
		require.NoError(t, err)
		require.Empty(t, attempts)
		query.Target.Portfile = "Portfile"
		query.Tree = ""
		_, err = r.VerificationCandidates(ctx, query)
		require.ErrorIs(t, err, state.ErrInvalid, "port-only lookup requires an exact tree")
		query.Target.Name = "fixture"
		query.Tree = record.ObjectID(strings.Repeat("c", 40))
		attempts, err = r.VerificationCandidates(ctx, query)
		require.NoError(t, err)
		require.Empty(t, attempts)
		query.Tree = ""
		query.Limit = 1
		attempts, err = r.VerificationCandidates(ctx, query)
		require.NoError(t, err)
		require.Equal(t, record.AttemptID("a-39"), attempts[0].ID)
		query.Limit = 33
		_, err = r.VerificationCandidates(ctx, query)
		require.ErrorIs(t, err, state.ErrInvalid)
		return nil
	}))
	require.NoError(t, s.Update(t.Context(), b.ID, func(ctx context.Context, tx state.Tx) error {
		job, err := tx.Job(ctx, two.JobID)
		require.NoError(t, err)
		now := time.Now()
		job.State = record.JobCompleted
		job.FinishedAt = &now
		job.ReusedAttempt = "a-00"
		require.ErrorIs(t, tx.PutJob(ctx, job), state.ErrNotFound)
		return nil
	}))
}
