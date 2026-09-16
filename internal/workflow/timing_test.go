package workflow_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestObservationScheduleSurvivesOtherDriversAndCancellationPreemptsIt(t *testing.T) {
	f := newFixture(t)
	f.engine.ObserveInterval = 30 * time.Second
	id := f.submit(t, "scheduled")
	f.run(t, id)
	require.Equal(t, f.now().Add(30*time.Second), *f.attempt(t, id).RetryAt)
	store, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
	require.NoError(t, err)
	defer store.Close()
	other := *f.engine
	other.State, other.Owner, other.ObserveInterval = store, "other", time.Millisecond
	scope := workflow.Scope{Jobs: []record.JobID{id}}
	f.advance(5 * time.Second)
	result, err := other.Cycle(t.Context(), scope)
	require.NoError(t, err)
	require.Empty(t, result.Advanced)
	require.Zero(t, f.provider.count("observe"))
	f.cancel(t, id)
	_, err = other.Cycle(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, 1, f.provider.count("cancel"), "cancellation must not wait for the observation deadline")
	f.provider.observe = terminal(f, record.VerdictCanceled)
	f.advance(time.Second)
	_, err = other.Cycle(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, record.JobCanceled, f.status(t, id).Jobs[0].Job.State)
}
func TestDueObservationIsClaimedByOnlyOneDriver(t *testing.T) {
	f := newFixture(t)
	f.engine.ObserveInterval = 10 * time.Second
	id := f.submit(t, "shared-observation")
	f.run(t, id)
	f.advance(10 * time.Second)
	started, proceed := make(chan struct{}), make(chan struct{})
	f.provider.observe = func(_ context.Context, run record.ProviderRun) (verify.Observation, error) {
		close(started)
		<-proceed
		return verify.Observation{Run: run, State: record.AttemptRunning, Verdict: record.VerdictUnknown, ObservedAt: f.now()}, nil
	}
	reply := startCycle(t.Context(), f.engine, id)
	receive(t, started)
	other := *f.engine
	other.Owner = "other"
	_, err := other.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
	close(proceed)
	require.NoError(t, err)
	require.NoError(t, receive(t, reply).err)
	require.Equal(t, 1, f.provider.count("observe"))
	require.Equal(t, f.now().Add(10*time.Second), *f.attempt(t, id).RetryAt)
}
func TestOperationDeadlinesHaveMatchingClaimsAndTimeoutsRemainRetryable(t *testing.T) {
	f := newFixture(t)
	f.engine.Timeouts.Provision = 2 * time.Second
	f.engine.Timeouts.Observe = 500 * time.Millisecond
	f.engine.Timeouts.Cleanup = time.Second
	f.engine.LeaseGrace = time.Second
	id := f.submit(t, "deadlines")
	check := func(ctx context.Context, budget time.Duration) {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.InDelta(t, budget.Seconds(), time.Until(deadline).Seconds(), 0.25)
	}
	f.provider.submit = func(ctx context.Context, r verify.Request) (verify.Submission, error) {
		check(ctx, 2*time.Second)
		require.Equal(t, f.now().Add(3*time.Second), f.attempt(t, id).Claim.ExpiresAt)
		return admitted(r.ID), nil
	}
	f.run(t, id)
	f.provider.observe = func(ctx context.Context, _ record.ProviderRun) (verify.Observation, error) {
		check(ctx, 500*time.Millisecond)
		require.Equal(t, f.now().Add(1500*time.Millisecond), f.attempt(t, id).Claim.ExpiresAt)
		<-ctx.Done()
		return verify.Observation{}, ctx.Err()
	}
	f.run(t, id)
	attempt := f.attempt(t, id)
	require.Equal(t, record.AttemptRunning, attempt.State)
	require.Nil(t, attempt.Claim)
	require.Contains(t, attempt.LastError, context.DeadlineExceeded.Error())
	require.Equal(t, f.now().Add(time.Second), *attempt.RetryAt)
	f.provider.observe = terminal(f, record.VerdictPassed)
	f.provider.release = func(ctx context.Context, _ record.ResourceHandle) (verify.ReleaseResult, error) {
		check(ctx, time.Second)
		return verify.ReleaseResult{Confirmed: true}, nil
	}
	f.run(t, id)
	require.Equal(t, record.JobCompleted, f.status(t, id).Jobs[0].Job.State)
}

func TestFailureBackoffSurvivesDriverRestartAndResetsOnSuccess(t *testing.T) {
	f := newFixture(t)
	id := f.submit(t, "backoff")
	f.run(t, id)
	f.provider.observe = func(context.Context, record.ProviderRun) (verify.Observation, error) {
		return verify.Observation{}, fmt.Errorf("temporary outage")
	}
	f.run(t, id)
	first := f.attempt(t, id)
	require.EqualValues(t, 1, first.ConsecutiveFailures)
	reopened, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
	require.NoError(t, err)
	defer reopened.Close()
	other := *f.engine
	other.State, other.Owner = reopened, "restarted-driver"
	f.advance(time.Second)
	_, err = other.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
	require.NoError(t, err)
	second := f.attempt(t, id)
	require.EqualValues(t, 2, second.ConsecutiveFailures)
	require.GreaterOrEqual(t, second.RetryAt.Sub(f.now()), 2*time.Second)
	calls := f.provider.count("observe")
	f.advance(time.Second)
	_, err = other.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
	require.NoError(t, err)
	require.Equal(t, calls, f.provider.count("observe"), "another driver must respect the stored deadline")
	f.advance(second.RetryAt.Sub(f.now()))
	f.provider.observe = func(_ context.Context, run record.ProviderRun) (verify.Observation, error) {
		return verify.Observation{Run: run, State: record.AttemptRunning, Verdict: record.VerdictUnknown, ObservedAt: f.now()}, nil
	}
	_, err = other.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
	require.NoError(t, err)
	require.Zero(t, f.attempt(t, id).ConsecutiveFailures)
}

func TestCapacityUsesWaitingScheduleWithoutFailureCount(t *testing.T) {
	f := newFixture(t)
	f.engine.WaitInterval = time.Minute
	f.provider.submit = func(context.Context, verify.Request) (verify.Submission, error) {
		return verify.Submission{State: verify.AtCapacity}, nil
	}
	id := f.submit(t, "capacity-schedule")
	f.run(t, id)
	attempt := f.attempt(t, id)
	require.Equal(t, f.now().Add(time.Minute), *attempt.RetryAt)
	require.Zero(t, attempt.ConsecutiveFailures)
	require.Empty(t, attempt.LastError)
	f.run(t, id)
	require.Equal(t, 1, f.provider.count("submit"))
	f.cancel(t, id)
	f.run(t, id)
	require.Equal(t, record.JobCanceled, f.status(t, id).Jobs[0].Job.State)
}

func TestMissingRegisteredProviderDoesNotUseFallbackOrTerminateWork(t *testing.T) {
	f := newFixture(t)
	id := f.submit(t, "missing-provider")
	f.engine.Providers = map[string]verify.Provider{}
	f.run(t, id)
	attempt := f.attempt(t, id)
	require.Contains(t, attempt.LastError, `provider "scripted" is unavailable in this driver`)
	require.Zero(t, f.provider.count("capabilities"))
	require.Zero(t, f.provider.count("submit"))
	require.Equal(t, record.JobActive, f.status(t, id).Jobs[0].Job.State)
	require.EqualValues(t, 1, attempt.ConsecutiveFailures)
	other := *f.engine
	other.Providers = map[string]verify.Provider{"scripted": f.provider}
	f.advance(time.Second)
	_, err := other.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
	require.NoError(t, err)
	require.Equal(t, record.AttemptRunning, f.attempt(t, id).State)
}

func TestDefaultSubmissionClaimCoversColdPreparationAndStartup(t *testing.T) {
	f := newFixture(t)
	f.engine.Timeouts.Provision = 0
	f.engine.LeaseGrace = time.Minute
	id := f.submit(t, "cold-submission")
	started, proceed := make(chan struct{}), make(chan struct{})
	f.provider.submit = func(ctx context.Context, request verify.Request) (verify.Submission, error) {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.Greater(t, time.Until(deadline), 10*time.Minute)
		close(started)
		<-proceed
		return admitted(request.ID), nil
	}
	reply := startCycle(t.Context(), f.engine, id)
	receive(t, started)
	f.advance(6 * time.Minute)
	other := *f.engine
	other.Owner = "other-driver"
	_, err := other.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
	close(proceed)
	require.NoError(t, err)
	require.NoError(t, receive(t, reply).err)
	require.Equal(t, 1, f.provider.count("submit"), "cold staging must not let another driver reclaim an active submission")
	require.Equal(t, record.AttemptRunning, f.attempt(t, id).State)
}
