package workflow

import (
	"errors"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestClaimGuardRejectsFinishedMovedOrForeignJobs(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	claim := &record.Claim{Owner: "driver", Generation: 2, ExpiresAt: now.Add(time.Minute)}
	job := record.Job{State: record.JobActive, Phase: record.PhaseVerification}
	job.Claim = claim
	require.NoError(t, claimGuard(job, record.PhaseVerification, claim, now))
	require.ErrorIs(t, claimGuard(job, record.PhasePublication, claim, now), ErrClaimLost, "the phase moved")
	require.ErrorIs(t, claimGuard(job, record.PhaseVerification, &record.Claim{Owner: "driver", Generation: 1, ExpiresAt: claim.ExpiresAt}, now), ErrClaimLost, "an older generation")
	require.ErrorIs(t, claimGuard(job, record.PhaseVerification, claim, now.Add(2*time.Minute)), ErrClaimLost, "an expired lease")
	job.State = record.JobCompleted
	require.ErrorIs(t, claimGuard(job, record.PhaseVerification, claim, now), ErrClaimLost, "a finished job")
}

func TestFinishJobReleasesTheLease(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	retry := now.Add(time.Minute)
	job := record.Job{State: record.JobActive}
	job.Claim = &record.Claim{Owner: "driver", Generation: 1, ExpiresAt: now.Add(time.Hour)}
	job.RetryAt = &retry
	job.ConsecutiveFailures = 4
	finishJob(&job, record.JobNeedsAttention, "why", now)
	require.Equal(t, record.JobNeedsAttention, job.State)
	require.Equal(t, "why", job.Detail)
	require.Equal(t, now, *job.FinishedAt)
	require.Nil(t, job.Claim)
	require.Nil(t, job.RetryAt)
}

func TestLeaseHelpersScheduleFailureAndWaiting(t *testing.T) {
	errTest := errors.New("test failure")
	engine := &Engine{RetryDelay: time.Second, WaitInterval: 10 * time.Second, Now: func() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) }}
	c, err := engine.newCycle()
	require.NoError(t, err)
	var lease record.Lease
	require.NoError(t, c.take(&lease, engine.now(), time.Minute))
	require.NotNil(t, lease.Claim)
	require.Equal(t, uint64(1), lease.ClaimGeneration)
	first := c.fail(&lease, "key", errTest)
	require.Equal(t, uint32(1), lease.ConsecutiveFailures)
	require.Equal(t, engine.now().Add(time.Second), first)
	second := c.fail(&lease, "key", errTest)
	require.Equal(t, uint32(2), lease.ConsecutiveFailures)
	require.True(t, second.After(first), "backoff grows")
	waiting := c.await(&lease)
	require.Zero(t, lease.ConsecutiveFailures, "expected waiting resets failures")
	require.Equal(t, engine.now().Add(10*time.Second), waiting)
	require.Equal(t, waiting, *lease.RetryAt)
}
