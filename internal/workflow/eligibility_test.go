package workflow_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/verify"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestIdleCycleDoesNotAcquireWriterOrCallProvider(t *testing.T) {
	for _, kind := range []string{"settled", "attempt claimed", "attempt delayed", "uncertain resource with running owner", "retained indefinitely", "retained until later", "cleanup claimed", "cleanup delayed"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			id := f.submit(t, "build")
			f.run(t, id)
			later := f.now().Add(time.Minute)
			require.NoError(t, f.mutate(t.Context(), func(_ context.Context, tx *fixtureTx) error {
				job := tx.State.Jobs[id]
				for key, attempt := range tx.State.Attempts {
					attempt.RetryAt = nil
					switch kind {
					case "attempt claimed":
						attempt.Claim = &record.Claim{Owner: "other", Generation: attempt.ClaimGeneration, ExpiresAt: later}
					case "attempt delayed", "uncertain resource with running owner":
						attempt.RetryAt = &later
					default:
						job.State = record.JobCompleted
						attempt.State = record.AttemptFinished
					}
					tx.State.Attempts[key] = attempt
				}
				tx.State.Jobs[id] = job
				for key, resource := range tx.State.Resources {
					switch kind {
					case "settled":
						resource.State = record.ResourceReleased
					case "uncertain resource with running owner":
						resource.State = record.ResourceUncertain
					case "retained indefinitely":
						resource.State = record.ResourceRetained
					case "retained until later":
						resource.State, resource.RetainUntil = record.ResourceRetained, &later
					case "cleanup claimed":
						resource.State = record.ResourceReleaseRequested
						resource.ClaimGeneration++
						resource.Claim = &record.Claim{Owner: "other", Generation: resource.ClaimGeneration, ExpiresAt: later}
					case "cleanup delayed":
						resource.State, resource.RetryAt = record.ResourceReleaseRequested, &later
					}
					tx.State.Resources[key] = resource
				}
				return nil
			}))
			before, err := f.snapshot(t.Context())
			require.NoError(t, err)
			calls := f.provider.count("capabilities")
			holder := f.holdWriter(t)
			defer holder.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
			defer cancel()
			result, err := f.engine.Cycle(ctx, workflow.Scope{All: true})
			require.NoError(t, err, "an idle cycle must complete while another process owns the writer lock")
			require.Empty(t, result.Advanced)
			require.Empty(t, result.Problems)
			require.Equal(t, calls, f.provider.count("capabilities"))
			after, err := f.snapshot(t.Context())
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestCancellationPreemptsQueuedRetryDelay(t *testing.T) {
	f := newFixture(t)
	f.provider.submit = func(context.Context, verify.Request) (verify.Submission, error) {
		return verify.Submission{State: verify.AtCapacity}, nil
	}
	id := f.submit(t, "queued")
	f.run(t, id)
	f.cancel(t, id)
	result, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
	require.NoError(t, err)
	require.Contains(t, result.Advanced, id)
	require.Equal(t, record.JobCanceled, f.status(t, id).Jobs[0].Job.State)
	require.Equal(t, 1, f.provider.count("submit"))
}

func TestPartiallyAppliedControlDoesNotWriteForUnselectedJobs(t *testing.T) {
	f := newFixture(t)
	a, b := f.submit(t, "first"), f.submit(t, "second")
	require.NoError(t, f.engine.Control(t.Context(), record.ControlRequest{ID: "cancel-both", Kind: record.Cancel, Jobs: []record.JobID{a, b}}))
	f.run(t, a)
	before, err := f.snapshot(t.Context())
	require.NoError(t, err)
	require.Nil(t, before.State.Controls["cancel-both"].AppliedAt)
	holder := f.holdWriter(t)
	defer holder.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	_, err = f.engine.Cycle(ctx, workflow.Scope{Jobs: []record.JobID{a}})
	cancel()
	require.NoError(t, err)
	require.NoError(t, holder.Close())
	f.run(t, b)
	after, err := f.snapshot(t.Context())
	require.NoError(t, err)
	require.NotNil(t, after.State.Controls["cancel-both"].AppliedAt)
	require.Equal(t, record.JobCanceled, after.State.Jobs[b].State)
}

type capabilityBarrier struct {
	verify.Provider
	started chan struct{}
	proceed chan struct{}
}

func (p *capabilityBarrier) Capabilities(ctx context.Context) (verify.Capabilities, error) {
	close(p.started)
	select {
	case <-p.proceed:
		return p.Provider.Capabilities(ctx)
	case <-ctx.Done():
		return verify.Capabilities{}, ctx.Err()
	}
}

func TestCandidateIsRecheckedAfterAnotherDriverAdvancesIt(t *testing.T) {
	f := newFixture(t)
	id := f.submit(t, "build")
	paused := &capabilityBarrier{Provider: f.provider, started: make(chan struct{}), proceed: make(chan struct{})}
	first := *f.engine
	first.Provider = paused
	reply := startCycle(t.Context(), &first, id)
	receive(t, paused.started)
	f.run(t, id)
	close(paused.proceed)
	result := receive(t, reply)
	require.NoError(t, result.err)
	require.Equal(t, []record.JobID{id}, result.result.Advanced) // The first driver recorded the plan before checking capabilities.
	require.Empty(t, result.result.Problems)
	require.Equal(t, 1, f.provider.count("submit"))
	require.Equal(t, record.AttemptRunning, f.attempt(t, id).State)
}

func TestCycleBoundsCandidateBatches(t *testing.T) {
	f := newFixture(t)
	f.provider.submit = func(context.Context, verify.Request) (verify.Submission, error) {
		return verify.Submission{State: verify.AtCapacity}, nil
	}
	for i := range 70 {
		f.submit(t, fmt.Sprint("job-", i))
	}
	first, err := f.engine.Cycle(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Len(t, first.Advanced, 64)
	second, err := f.engine.Cycle(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Len(t, second.Advanced, 6)
	third, err := f.engine.Cycle(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Empty(t, third.Advanced)
	require.Equal(t, 70, f.provider.count("submit"))
	status, err := f.engine.Status(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	require.Len(t, status.Jobs, 70)
}

func TestUnsupportedExecutorsCannotStarveOtherJobs(t *testing.T) {
	f := newFixture(t)
	for i := range 65 {
		request := f.request(fmt.Sprint("bump-", i))
		request.Spec.Action = record.Bump
		_, err := f.engine.Submit(t.Context(), request)
		require.NoError(t, err)
	}
	good := f.submit(t, "verify")
	for range 2 {
		_, err := f.engine.Cycle(t.Context(), workflow.Scope{All: true})
		require.NoError(t, err)
	}
	all, err := f.engine.Status(t.Context(), workflow.Scope{All: true})
	require.NoError(t, err)
	for _, v := range all.Jobs {
		if v.Job.ID != good {
			require.Equal(t, record.JobNeedsAttention, v.Job.State)
		}
	}
	require.Equal(t, record.AttemptRunning, f.attempt(t, good).State)
}
