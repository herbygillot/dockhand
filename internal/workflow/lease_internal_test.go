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
	waiting, spent := c.await(&lease, waitCapacity)
	require.False(t, spent)
	require.Zero(t, lease.ConsecutiveFailures, "expected waiting resets failures")
	require.Equal(t, uint32(1), lease.ConsecutiveWaits)
	require.Equal(t, engine.now().Add(10*time.Second), waiting)
	require.Equal(t, waiting, *lease.RetryAt)
	require.Equal(t, "capacity", lease.WaitKind)
	c.fail(&lease, "key", errTest)
	require.Zero(t, lease.ConsecutiveWaits, "a failure forgets waits")
	require.Empty(t, lease.WaitKind)
}

func TestWaitCountBelongsToOneKind(t *testing.T) {
	engine := &Engine{RetryDelay: time.Second, WaitInterval: 10 * time.Second, ObserveInterval: 30 * time.Second, Now: func() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) }}
	c, err := engine.newCycle()
	require.NoError(t, err)
	var lease record.Lease
	for i := 0; i < 15; i++ {
		_, spent := c.await(&lease, waitReconciliation)
		require.False(t, spent)
	}
	require.Equal(t, uint32(15), lease.ConsecutiveWaits)
	_, spent := c.await(&lease, waitBuild)
	require.False(t, spent)
	require.Equal(t, uint32(1), lease.ConsecutiveWaits, "a wait of another kind starts the count over")
	require.Equal(t, "build", lease.WaitKind)
	for i := 0; i < 20; i++ {
		_, spent = c.await(&lease, waitForge)
	}
	require.False(t, spent, "the twentieth forge wait is within budget")
	_, spent = c.await(&lease, waitForge)
	require.True(t, spent, "budgets count only waits of their own kind")
}

func TestWaitKindsScheduleAndBudgetDifferently(t *testing.T) {
	engine := &Engine{RetryDelay: time.Second, WaitInterval: 10 * time.Second, ObserveInterval: 30 * time.Second, Now: func() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) }}
	c, err := engine.newCycle()
	require.NoError(t, err)
	now := engine.now()
	for kind, interval := range map[waitKind]time.Duration{waitCapacity: 10 * time.Second, waitBuild: 30 * time.Second, waitCancellation: time.Second} {
		var lease record.Lease
		for i := 0; i < 50; i++ {
			retry, spent := c.await(&lease, kind)
			require.False(t, spent, "%s never exhausts", kind)
			require.Equal(t, now.Add(interval), retry, "%s keeps a flat interval", kind)
		}
		require.Equal(t, uint32(50), lease.ConsecutiveWaits)
	}
	for _, kind := range []waitKind{waitReconciliation, waitForge, waitRelease} {
		var lease record.Lease
		var last time.Time
		spentAt := 0
		for i := 1; i <= 25; i++ {
			retry, spent := c.await(&lease, kind)
			if i > 1 && retry.Before(last) {
				t.Fatalf("%s wait shrank at %d", kind, i)
			}
			require.LessOrEqual(t, retry.Sub(now), 5*time.Minute, "%s respects the ceiling", kind)
			last = retry
			if spent && spentAt == 0 {
				spentAt = i
			}
		}
		require.Equal(t, 21, spentAt, "%s exhausts after its budget of 20", kind)
	}
	require.Equal(t, "no progress after 21 forge waits: still waiting", exhausted(waitForge, 21, "still waiting"))
}
