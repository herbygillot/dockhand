package workflow_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state/sqlite"
	"github.com/herbygillot/dockhand/internal/verify"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestCycleCapacityAdmissionCompletionAndCleanup(t *testing.T) {
	f := newFixture(t)
	id := f.submit(t, "build")
	f.provider.submit = func(_ context.Context, r verify.Request) (verify.Submission, error) {
		if f.provider.count("submit") == 1 {
			return verify.Submission{State: verify.AtCapacity}, nil
		}
		return admitted(r.ID), nil
	}
	f.run(t, id)
	first := f.attempt(t, id)
	require.Equal(t, record.AttemptQueued, first.State, "capacity implied admission")
	require.Nil(t, f.status(t, id).Jobs[0].Job.AdmittedAt, "capacity implied admission")
	_, err := f.engine.Cycle(t.Context(), workflow.Scope{Jobs: []record.JobID{id}})
	require.NoError(t, err)
	require.Equal(t, 1, f.provider.count("submit"), "retry delay ignored")
	f.run(t, id)
	running := f.attempt(t, id)
	require.Equal(t, record.AttemptRunning, running.State, "capacity retry changed identity or failed admission")
	require.Equal(t, first.SubmissionID, running.SubmissionID, "capacity retry changed identity or failed admission")
	require.NotNil(t, f.status(t, id).Jobs[0].Job.AdmittedAt, "confirmed admission not recorded")
	f.run(t, id)
	require.Equal(t, record.VerdictUnknown, f.attempt(t, id).Evidence.Verdict, "running observation became a pass")
	f.provider.observe = terminal(f, record.VerdictPassed)
	f.provider.release = func(context.Context, record.ResourceHandle) (verify.ReleaseResult, error) {
		return verify.ReleaseResult{Confirmed: f.provider.count("release") > 1}, nil
	}
	result := f.run(t, id)
	status := f.status(t, id)
	require.Equal(t, record.JobCompleted, status.Jobs[0].Job.State, "completion lost pending cleanup")
	require.Len(t, result.PendingCleanup, 1, "completion lost pending cleanup")
	require.Equal(t, record.ResourceUncertain, status.Resources[0].State, "completion lost pending cleanup")
	finished := *status.Jobs[0].Job.FinishedAt
	reopened, err := sqlite.Open(t.Context(), f.store.Path(), sqlite.Options{})
	t.Cleanup(func() { reopened.Close() })
	require.NoError(t, err)
	fresh := *f.engine
	fresh.State = reopened
	f.engine = &fresh
	f.run(t, id)
	status = f.status(t, id)
	require.Equal(t, record.ResourceReleased, status.Resources[0].State, "cleanup changed outcome or did not recover")
	require.NotNil(t, status.Jobs[0].Job.FinishedAt, "cleanup removed completion time")
	require.WithinDuration(t, finished, *status.Jobs[0].Job.FinishedAt, 0, "cleanup changed outcome or did not recover")
	before, err := f.snapshot(t.Context())
	require.NoError(t, err)
	f.run(t, id)
	after, err := f.snapshot(t.Context())
	require.NoError(t, err)
	require.Equal(t, before.Version, after.Version, "settled job repeated external work")
	require.Equal(t, 2, f.provider.count("submit"), "settled job repeated external work")
	require.Equal(t, 2, f.provider.count("release"), "settled job repeated external work")
}

func TestCycleRejectsInvalidEvidenceAndRetainsFailure(t *testing.T) {
	f := newFixture(t)
	id := f.submit(t, "evidence")
	f.run(t, id)
	for _, kind := range []string{"wrong run", "missing verdict", "missing time", "contradictory pass", "older observation"} {
		t.Run(kind, func(t *testing.T) {
			f.provider.observe = func(_ context.Context, r record.ProviderRun) (verify.Observation, error) {
				o := verify.Observation{Run: r, State: record.AttemptFinished, Verdict: record.VerdictPassed, ObservedAt: f.now()}
				switch kind {
				case "wrong run":
					o.Run.RunID = "another"
				case "missing verdict":
					o.Verdict = ""
				case "missing time":
					o.ObservedAt = time.Time{}
				case "contradictory pass":
					o.Steps = []record.StepResult{{Verdict: record.VerdictFailed}}
				case "older observation":
					o.ObservedAt = f.now().Add(-time.Hour)
				}
				return o, nil
			}
			if kind == "older observation" {
				f.provider.observe = nil
				f.run(t, id)
				f.provider.observe = func(_ context.Context, r record.ProviderRun) (verify.Observation, error) {
					return verify.Observation{Run: r, State: record.AttemptFinished, Verdict: record.VerdictPassed, ObservedAt: f.now().Add(-time.Hour)}, nil
				}
			}
			result := f.run(t, id)
			require.Len(t, result.Problems, 1, "invalid evidence completed job: %+v", result)
			require.Equal(t, record.JobActive, f.status(t, id).Jobs[0].Job.State, "invalid evidence completed job: %+v", result)
		})
	}
	f.provider.observe = func(_ context.Context, r record.ProviderRun) (verify.Observation, error) {
		return verify.Observation{Run: r, State: record.AttemptFinished, Verdict: record.VerdictFailed, ObservedAt: f.now(), Failure: &record.Failure{Kind: record.DependencyFailure, Package: "outside-cohort", DependencyChain: []string{"fixture", "outside-cohort"}, Attribution: record.AttributionUnknown}}, nil
	}
	f.run(t, id)
	f.cancel(t, id)
	f.run(t, id)
	status := f.status(t, id)
	require.Equal(t, record.JobFailed, status.Jobs[0].Job.State, "failure diagnosis was lost")
	require.Equal(t, record.ResourceRetained, status.Resources[0].State, "failure diagnosis was lost")
	require.Equal(t, "outside-cohort", status.Jobs[0].Attempts[0].Evidence.Failure.Package, "failure was hidden, cleaned or rebuilt")
	require.Zero(t, f.provider.count("release"), "failure was hidden, cleaned or rebuilt")
	require.Equal(t, 1, f.provider.count("submit"), "failure was hidden, cleaned or rebuilt")
}

func TestCycleReconcilesUncertainSubmission(t *testing.T) {
	f := newFixture(t)
	id := f.submit(t, "uncertain")
	f.provider.submit = func(_ context.Context, r verify.Request) (verify.Submission, error) {
		return admitted(r.ID), errors.New("lost acknowledgement")
	}
	f.run(t, id)
	attempt := f.attempt(t, id)
	require.Equal(t, record.AttemptUncertain, attempt.State, "uncertain submission was admitted")
	require.Nil(t, f.status(t, id).Jobs[0].Job.AdmittedAt, "uncertain submission was admitted")
	f.run(t, id)
	require.Equal(t, 1, f.provider.count("submit"), "unknown outcome was resubmitted")
	require.Equal(t, 1, f.provider.count("reconcile"), "unknown outcome was resubmitted")
	f.provider.reconcile = func(_ context.Context, request record.RequestID) (verify.Reconciliation, error) {
		return verify.Reconciliation{State: verify.RunFound, Submission: admitted(request)}, nil
	}
	f.run(t, id)
	recovered := f.attempt(t, id)
	require.Equal(t, record.AttemptRunning, recovered.State, "recovery duplicated the original run")
	require.Equal(t, attempt.SubmissionID, recovered.SubmissionID, "recovery duplicated the original run")
	require.Equal(t, 1, f.provider.count("submit"), "recovery duplicated the original run")
}

func TestCycleClosedSubmissionRetryOrPartialCleanup(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "partial provisioning"}[partial], func(t *testing.T) {
			f := newFixture(t)
			id := f.submit(t, "closed")
			f.provider.submit = func(_ context.Context, r verify.Request) (verify.Submission, error) {
				response := verify.Submission{State: verify.SubmissionUncertain}
				if partial {
					response.Resources = admitted(r.ID).Resources
				}
				return response, nil
			}
			f.run(t, id)
			old := f.attempt(t, id).SubmissionID
			f.provider.reconcile = func(context.Context, record.RequestID) (verify.Reconciliation, error) {
				return verify.Reconciliation{State: verify.RequestClosed}, nil
			}
			f.run(t, id)
			attempt := f.attempt(t, id)
			require.Len(t, f.closed(t, attempt.ID), 1, "closed identity lost")
			require.Equal(t, old, f.closed(t, attempt.ID)[0], "closed identity lost")
			if partial {
				require.Equal(t, record.JobNeedsAttention, f.status(t, id).Jobs[0].Job.State, "partial resources not drained")
				require.Equal(t, record.ResourceReleased, f.status(t, id).Resources[0].State, "partial resources not drained")
				return
			}
			require.NotEqual(t, old, attempt.SubmissionID, "closed identity reused")
			require.Equal(t, record.AttemptQueued, attempt.State, "closed identity reused")
			f.provider.submit = nil
			f.run(t, id)
			require.Equal(t, record.AttemptRunning, f.attempt(t, id).State, "fresh identity was not admitted")
		})
	}
}

func TestCycleCancellationNeedsAnObservedOutcome(t *testing.T) {
	for _, verdict := range []record.Verdict{record.VerdictCanceled, record.VerdictPassed} {
		t.Run(string(verdict), func(t *testing.T) {
			f := newFixture(t)
			id := f.submit(t, "cancel")
			f.run(t, id)
			f.cancel(t, id)
			f.run(t, id)
			require.NotNil(t, f.attempt(t, id).CancelSentAt, "cancel acknowledgement fabricated completion")
			require.Equal(t, record.JobActive, f.status(t, id).Jobs[0].Job.State, "cancel acknowledgement fabricated completion")
			require.Zero(t, f.provider.count("release"), "cancel acknowledgement fabricated completion")
			f.provider.observe = terminal(f, verdict)
			f.run(t, id)
			status := f.status(t, id)
			expected := record.JobCanceled
			if verdict == record.VerdictPassed {
				expected = record.JobCompleted
			}
			require.Equal(t, expected, status.Jobs[0].Job.State, "observed outcome lost: %+v", status.Jobs[0].Job)
			require.Equal(t, record.ResourceReleased, status.Resources[0].State, "observed outcome lost: %+v", status.Jobs[0].Job)
		})
	}
	t.Run("failed cancel still observes", func(t *testing.T) {
		f := newFixture(t)
		id := f.submit(t, "cancel-error")
		f.run(t, id)
		f.cancel(t, id)
		f.provider.cancel = func(context.Context, record.ProviderRun) error { return errors.New("no acknowledgement") }
		f.run(t, id)
		f.provider.observe = terminal(f, record.VerdictCanceled)
		f.run(t, id)
		require.Equal(t, record.JobCanceled, f.status(t, id).Jobs[0].Job.State, "cancel failure starved observation")
		require.Equal(t, 1, f.provider.count("cancel"), "cancel failure starved observation")
	})
}

func TestCycleOneUnsupportedJobDoesNotBlockAnother(t *testing.T) {
	f := newFixture(t)
	bad := f.request("missing-config")
	bad.Spec.Build = nil
	receipt, err := f.engine.Submit(t.Context(), bad)
	require.NoError(t, err)
	good := f.submit(t, "good")
	result := f.run(t)
	require.Len(t, result.Problems, 1, "unsupported job stopped independent work: %+v", result)
	require.Equal(t, record.JobNeedsAttention, f.status(t, receipt.JobID).Jobs[0].Job.State, "unsupported job stopped independent work: %+v", result)
	require.Equal(t, record.AttemptRunning, f.attempt(t, good).State, "unsupported job stopped independent work: %+v", result)
}

func TestCycleExpiredClaimRejectsStaleResult(t *testing.T) {
	f := newFixture(t)
	id := f.submit(t, "claims")
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	f.provider.submit = func(ctx context.Context, r verify.Request) (verify.Submission, error) {
		close(entered)
		select {
		case <-release:
			return admitted(r.ID), nil
		case <-ctx.Done():
			return verify.Submission{}, ctx.Err()
		}
	}
	first := startCycle(t.Context(), f.engine, id)
	receive(t, entered)
	require.Equal(t, record.AttemptSubmitting, f.attempt(t, id).State, "provider call preceded durable intent")
	f.run(t, id)
	require.Equal(t, 1, f.provider.count("submit"), "live claim admitted another caller")
	require.Zero(t, f.provider.count("reconcile"), "live claim admitted another caller")
	f.advance(2 * time.Minute)
	f.provider.reconcile = func(_ context.Context, r record.RequestID) (verify.Reconciliation, error) {
		return verify.Reconciliation{State: verify.RunFound, Submission: admitted(r)}, nil
	}
	f.run(t, id)
	unblock.Do(func() { close(release) })
	reply := receive(t, first)
	require.NoError(t, reply.err)
	require.Len(t, reply.result.Problems, 1, "stale result was not rejected: %+v", reply.result)
	require.Equal(t, workflow.ErrClaimLost.Error(), reply.result.Problems[0].Detail, "stale result was not rejected: %+v", reply.result)
	current := f.attempt(t, id)
	require.Equal(t, record.AttemptRunning, current.State, "stale claim replaced recovered state")
	require.Nil(t, current.Claim, "stale claim replaced recovered state")
	require.Equal(t, uint64(2), current.ClaimGeneration, "stale claim replaced recovered state")
}

func TestCycleCancellationClosesIdentityBeforeLateSubmission(t *testing.T) {
	f := newFixture(t)
	id := f.submit(t, "late")
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	var closed atomic.Bool
	var runs atomic.Int64
	f.provider.submit = func(ctx context.Context, r verify.Request) (verify.Submission, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return verify.Submission{}, ctx.Err()
		}
		if closed.Load() {
			return verify.Submission{State: verify.Unsupported}, nil
		}
		runs.Add(1)
		return admitted(r.ID), nil
	}
	f.provider.reconcile = func(context.Context, record.RequestID) (verify.Reconciliation, error) {
		closed.Store(true)
		return verify.Reconciliation{State: verify.RequestClosed}, nil
	}
	first := startCycle(t.Context(), f.engine, id)
	receive(t, entered)
	f.cancel(t, id)
	f.advance(2 * time.Minute)
	f.run(t, id)
	require.Equal(t, record.JobCanceled, f.status(t, id).Jobs[0].Job.State, "cancellation did not close uncertain submission")
	require.True(t, closed.Load(), "cancellation did not close uncertain submission")
	unblock.Do(func() { close(release) })
	reply := receive(t, first)
	require.NoError(t, reply.err)
	require.Zero(t, runs.Load(), "late driver created an untracked run")
	require.Len(t, reply.result.Problems, 1, "late driver created an untracked run")
	require.Empty(t, f.status(t, id).Resources, "late driver created an untracked run")
}

func TestCycleCleanupHasIndependentClaims(t *testing.T) {
	f := newFixture(t)
	id := f.submit(t, "cleanup")
	f.run(t, id)
	f.provider.observe = terminal(f, record.VerdictPassed)
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	f.provider.release = func(ctx context.Context, _ record.ResourceHandle) (verify.ReleaseResult, error) {
		if f.provider.count("release") == 1 {
			close(entered)
			select {
			case <-release:
				return verify.ReleaseResult{Confirmed: false}, nil
			case <-ctx.Done():
				return verify.ReleaseResult{}, ctx.Err()
			}
		}
		return verify.ReleaseResult{Confirmed: true}, nil
	}
	f.advance(2 * time.Second)
	first := startCycle(t.Context(), f.engine, id)
	receive(t, entered)
	require.Equal(t, record.JobCompleted, f.status(t, id).Jobs[0].Job.State, "job completion waited for cleanup")
	f.run(t, id)
	require.Equal(t, 1, f.provider.count("release"), "live cleanup claim duplicated release")
	f.advance(2 * time.Minute)
	f.run(t, id)
	unblock.Do(func() { close(release) })
	reply := receive(t, first)
	require.NoError(t, reply.err)
	require.Len(t, reply.result.Problems, 1, "stale cleanup undid confirmation")
	require.Equal(t, record.ResourceReleased, f.status(t, id).Resources[0].State, "stale cleanup undid confirmation")
}

func TestCycleRunsEveryVerificationTargetAfterOneFails(t *testing.T) {
	f := newFixture(t)
	request := f.request("multiple-targets")
	request.Spec.Targets = append(request.Spec.Targets, record.Target{Name: "dependent", Portfile: "devel/dependent/Portfile"})
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)

	var observations atomic.Int32
	f.provider.observe = func(_ context.Context, run record.ProviderRun) (verify.Observation, error) {
		verdict := record.VerdictPassed
		if observations.Add(1) == 1 {
			verdict = record.VerdictFailed
		}
		return verify.Observation{Run: run, State: record.AttemptFinished, Verdict: verdict, ObservedAt: f.now()}, nil
	}

	sawPartialFailure := false
	for range 8 {
		f.run(t, receipt.JobID)
		status := f.status(t, receipt.JobID).Jobs[0]
		failed, unfinished := 0, 0
		for _, attempt := range status.Attempts {
			if !attempt.State.Terminal() {
				unfinished++
			}
			if attempt.Evidence != nil && attempt.Evidence.Verdict == record.VerdictFailed {
				failed++
			}
		}
		if failed == 1 && unfinished > 0 {
			sawPartialFailure = true
			require.Equal(t, record.JobActive, status.Job.State)
		}
		if status.Job.State.Terminal() {
			break
		}
	}

	status := f.status(t, receipt.JobID).Jobs[0]
	require.True(t, sawPartialFailure, "the first failed target ended the job before independent work ran")
	require.Equal(t, record.JobFailed, status.Job.State)
	require.Len(t, status.Attempts, 2)
	require.Equal(t, 2, f.provider.count("submit"))
	verdicts := map[record.Verdict]int{}
	targets := map[string]bool{}
	for _, attempt := range status.Attempts {
		require.True(t, attempt.State.Terminal())
		require.NotNil(t, attempt.Evidence)
		verdicts[attempt.Evidence.Verdict]++
		targets[attempt.Spec.Target.Name] = true
	}
	require.Equal(t, map[record.Verdict]int{record.VerdictFailed: 1, record.VerdictPassed: 1}, verdicts)
	require.Equal(t, map[string]bool{"dependent": true, "fixture": true}, targets)
	require.Contains(t, status.Job.Detail, "1 failed")
	require.Contains(t, status.Job.Detail, "1 passed")
}

func TestCycleCancelsEveryVerificationTarget(t *testing.T) {
	f := newFixture(t)
	request := f.request("cancel-multiple")
	request.Spec.Targets = append(request.Spec.Targets, record.Target{Name: "dependent", Portfile: "devel/dependent/Portfile"})
	receipt, err := f.engine.Submit(t.Context(), request)
	require.NoError(t, err)

	f.run(t, receipt.JobID)
	f.run(t, receipt.JobID)
	status := f.status(t, receipt.JobID).Jobs[0]
	require.Len(t, status.Attempts, 2)
	for _, attempt := range status.Attempts {
		require.Equal(t, record.AttemptRunning, attempt.State)
	}

	f.cancel(t, receipt.JobID)
	f.provider.observe = terminal(f, record.VerdictCanceled)
	for range 6 {
		f.run(t, receipt.JobID)
		if f.status(t, receipt.JobID).Jobs[0].Job.State.Terminal() {
			break
		}
	}

	status = f.status(t, receipt.JobID).Jobs[0]
	require.Equal(t, record.JobCanceled, status.Job.State)
	require.Equal(t, 2, f.provider.count("cancel"))
	for _, attempt := range status.Attempts {
		require.Equal(t, record.AttemptCanceled, attempt.State)
		require.Equal(t, record.VerdictCanceled, attempt.Evidence.Verdict)
	}
}

func TestPartialSubmissionCleanupPreservesItsFailure(t *testing.T) {
	f := newFixture(t)
	id := f.submit(t, "failed-staging")
	f.provider.submit = func(_ context.Context, request verify.Request) (verify.Submission, error) {
		return verify.Submission{State: verify.SubmissionUncertain, Resources: admitted(request.ID).Resources}, errors.New("indexing selected source failed")
	}
	f.run(t, id)
	f.provider.reconcile = func(context.Context, record.RequestID) (verify.Reconciliation, error) {
		return verify.Reconciliation{State: verify.RequestClosed}, nil
	}
	f.run(t, id)
	status := f.status(t, id)
	require.Equal(t, record.JobNeedsAttention, status.Jobs[0].Job.State)
	require.Contains(t, status.Jobs[0].Job.Detail, "indexing selected source failed")
	require.Contains(t, status.Jobs[0].Attempts[0].LastError, "indexing selected source failed")
	require.Equal(t, record.ResourceReleased, status.Resources[0].State)
}
