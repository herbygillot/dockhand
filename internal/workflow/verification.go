package workflow

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/verify"
)

// attemptAction selects one provider operation after the attempt has been claimed.
type attemptAction string

const (
	submitAttempt    attemptAction = "submit"
	reconcileAttempt attemptAction = "reconcile"
	observeAttempt   attemptAction = "observe"
	cancelAttempt    attemptAction = "cancel"
)

// attemptResult carries one provider response back across the transaction
// boundary. Only the response corresponding to the selected action is used.
type attemptResult struct {
	submission     verify.Submission
	reconciliation verify.Reconciliation
	observation    verify.Observation
	err            error
}

// advanceJob settles local-only work or claims, calls, and records one attempt
// action. It returns whether advancement was confirmed, a per-job problem,
// and an error if the caller must handle a lost claim or stop the pass. A later
// failure can leave the earlier claim transaction committed for recovery.
func (c *cycle) advanceJob(ctx context.Context, id record.JobID) (bool, string, error) {
	e := c.engine
	var attempt record.Attempt
	var action attemptAction
	var changed, recorded bool
	var detail string
	var err error
	for {
		needsProvider := false
		err = e.updateExecution(ctx, id, func(tx state.Tx, work *execution) error {
			job := work.Job
			if jobTerminal(job.State) {
				return nil
			}
			now := e.now()
			attempt = work.Attempt
			if work.Problem != "" {
				detail = work.Problem
				job.State, job.FinishedAt, job.Detail = record.JobNeedsAttention, &now, detail
				work.Job = job
				changed = true
				return nil
			}

			if job.CancelRequestedAt != nil && (attempt.ID == "" || attempt.State == record.AttemptQueued) {
				if live(attempt.Claim, now) {
					return nil
				}
				if attempt.ID != "" {
					if attempt.Run != (record.ProviderRun{}) || hasResources(work, attempt.ID) {
						return fmt.Errorf("%w: queued attempt has external effects", state.ErrInvalid)
					}
					finishAttempt(work, &job, &attempt, record.Evidence{Verdict: record.VerdictCanceled, ObservedAt: now}, "Canceled before admission", now)
				} else {
					job.State, job.FinishedAt, job.Detail = record.JobCanceled, &now, "Canceled before admission"
					work.Job = job
				}
				changed = true
				return nil
			}
			if !verificationJob(job) {
				detail = ErrNotImplemented.Error()
				job.State, job.FinishedAt, job.Detail = record.JobNeedsAttention, &now, detail
				work.Job = job
				changed = true
				return nil
			}
			if attempt.ID == "" {
				plan, build, err := verify.PlanSingle(job, work.Revision)
				if err != nil {
					job.State, job.FinishedAt, job.Detail = record.JobNeedsAttention, &now, err.Error()
					work.Job = job
					changed, detail = true, err.Error()
					return nil
				}
				reused, explanation, err := selectVerification(ctx, tx, job, build)
				if err != nil {
					return err
				}
				job.ReuseDetail = explanation
				if reused.ID != "" {
					job.ReusedAttempt = reused.ID
					job.State, job.FinishedAt, job.Detail = record.JobCompleted, &now, explanation
					work.Job, work.Plan = job, &plan
					changed = true
					return nil
				}
				attempt = record.Attempt{ID: record.AttemptID("attempt_" + rand.Text()), JobID: id, TargetID: plan.Targets[0].ID, Spec: build, State: record.AttemptQueued, CreatedAt: now}
				attempt.SubmissionID = record.RequestID("submit_" + string(attempt.ID))
				work.Submission = record.Submission{ID: attempt.SubmissionID, AttemptID: attempt.ID, Sequence: 1, Provider: build.Config.Provider, CreatedAt: now}
				work.Plan = &plan
				work.Attempt = attempt
				job.State = record.JobActive
				work.Job = job
				changed = true
			}
			if attemptTerminal(attempt.State) {
				return fmt.Errorf("%w: terminal attempt belongs to active job", state.ErrInvalid)
			}
			if live(attempt.Claim, now) || !due(attempt.RetryAt, now) {
				return nil
			}
			switch attempt.State {
			case record.AttemptQueued:
				action = submitAttempt
			case record.AttemptSubmitting, record.AttemptUncertain:
				action = reconcileAttempt
			case record.AttemptRunning:
				action = observeAttempt
				if job.CancelRequestedAt != nil && attempt.CancelSentAt == nil && !attempt.CancelPendingObservation {
					action = cancelAttempt
				}
			default:
				return fmt.Errorf("%w: unsupported attempt state %q", state.ErrInvalid, attempt.State)
			}
			if attempt.SubmissionID == "" {
				return fmt.Errorf("%w: attempt has no submission identity", state.ErrInvalid)
			}
			if !c.providerChecked {
				needsProvider, action = true, ""
				return nil
			}
			if err := c.providerReady(attempt.Spec.Config, action == submitAttempt); err != nil {
				detail = err.Error()
				retry := now.Add(c.retry)
				attempt.LastError, attempt.RetryAt = detail, &retry
				work.Attempt = attempt
				changed, action = true, ""
				return nil
			}
			claim, err := c.claim(&attempt.ClaimGeneration, now, c.attemptTimeout(action))
			if err != nil {
				return err
			}
			attempt.Claim = claim
			if action == submitAttempt {
				attempt.State = record.AttemptSubmitting
			}
			work.Attempt = attempt
			changed = true
			return nil
		})
		if err != nil {
			return recorded, detail, err
		}
		recorded = changed
		if !needsProvider {
			break
		}
		// Resolve capabilities after local planning, then recheck ownership and intent.
		c.checkProvider(ctx)
	}
	if action == "" {
		return changed, detail, nil
	}
	// The intent and claim are durable before any provider effect. Losing the
	// response leaves enough identity for another cycle to reconcile the call.
	response := c.callAttempt(ctx, action, attempt)
	// Re-read after the external call; the snapshot that authorized it may
	// have been superseded while this driver was waiting.
	err = e.updateExecution(ctx, id, func(tx state.Tx, work *execution) error {
		current := work.Attempt
		now := e.now()
		if current.ID != attempt.ID || !owns(current.Claim, attempt.Claim, now) {
			return ErrClaimLost
		}
		job := work.Job
		if jobTerminal(job.State) || current.State != attempt.State {
			return ErrClaimLost
		}
		current.Claim = nil
		current.LastError, current.RetryAt = "", nil
		detail = c.recordAttempt(work, &job, &current, action, response, now)
		if !attemptTerminal(current.State) {
			delay := c.retry
			if current.State == record.AttemptRunning && detail == "" && job.CancelRequestedAt == nil {
				delay = c.observe
			}
			retry := now.Add(delay)
			current.RetryAt = &retry
		}
		current.LastError = detail
		work.Attempt = current
		work.Job = job
		return nil
	})
	return changed, detail, err
}

// callAttempt performs exactly one provider operation outside the write transaction.
// It reports a context error even when the provider returns nil after expiry,
// so a late result is not accepted as timely confirmation.
func (c *cycle) callAttempt(ctx context.Context, action attemptAction, attempt record.Attempt) attemptResult {
	ctx, cancel := context.WithTimeout(ctx, c.attemptTimeout(action))
	defer cancel()
	var result attemptResult
	switch action {
	case submitAttempt:
		result.submission, result.err = c.engine.Provider.Submit(ctx, verify.Request{ID: attempt.SubmissionID, AttemptID: attempt.ID, Spec: attempt.Spec})
	case reconcileAttempt:
		result.reconciliation, result.err = c.engine.Provider.Reconcile(ctx, attempt.SubmissionID)
	case observeAttempt:
		result.observation, result.err = c.engine.Provider.Observe(ctx, attempt.Run)
	case cancelAttempt:
		result.err = c.engine.Provider.Cancel(ctx, attempt.Run)
	}
	if result.err == nil {
		result.err = ctx.Err()
	}
	return result
}

// recordAttempt applies a provider response within the caller's transaction
// after claim validation. Its returned detail is stored on the attempt and
// reported as a job problem. It performs no provider calls.
func (c *cycle) recordAttempt(work *execution, job *record.Job, attempt *record.Attempt, action attemptAction, result attemptResult, now time.Time) string {
	switch action {
	case submitAttempt:
		return recordSubmission(work, job, attempt, result.submission, result.err, now)
	case reconcileAttempt:
		if result.err != nil {
			return result.err.Error()
		}
		switch result.reconciliation.State {
		case verify.RunFound:
			submission := result.reconciliation.Submission
			if submission.State != verify.Admitted {
				submission.State = verify.SubmissionUncertain
			}
			return recordSubmission(work, job, attempt, submission, nil, now)
		case verify.RequestClosed:
			// Closure must fence late submissions at the provider. An observation
			// of temporary absence is insufficient to cancel or retry safely.
			if result.reconciliation.Submission.Run != (record.ProviderRun{}) || attempt.Run != (record.ProviderRun{}) {
				return "workflow: closed submission unexpectedly identifies a run"
			}
			if err := recordResources(work, *attempt, result.reconciliation.Submission.Resources); err != nil {
				return err.Error()
			}
			work.Submission.ClosedAt = &now
			if hasResources(work, attempt.ID) {
				finishAttempt(work, job, attempt, record.Evidence{Verdict: record.VerdictErrored, ObservedAt: now}, "Submission closed after partial provisioning", now)
				dispositionResources(work, attempt.ID, record.ResourceReleaseRequested)
				return job.Detail
			}
			if job.CancelRequestedAt != nil {
				finishAttempt(work, job, attempt, record.Evidence{Verdict: record.VerdictCanceled, ObservedAt: now}, "Canceled before admission", now)
			} else {
				attempt.SubmissionID = record.RequestID("submit_" + rand.Text())
				work.NextSubmission = &record.Submission{ID: attempt.SubmissionID, AttemptID: attempt.ID, Sequence: work.Submission.Sequence + 1, Provider: attempt.Spec.Config.Provider, CreatedAt: now}
				attempt.State = record.AttemptQueued
			}
			return ""
		case verify.RunUnknown:
			attempt.State = record.AttemptUncertain
			return "workflow: provider cannot yet determine whether submission exists"
		default:
			return "workflow: invalid provider reconciliation state"
		}
	case cancelAttempt:
		// A successful call acknowledges intent. Observation must still
		// establish the outcome, including when the cancellation call failed.
		attempt.CancelPendingObservation = true
		if result.err != nil {
			return result.err.Error()
		}
		attempt.CancelSentAt = &now
		return ""
	case observeAttempt:
		attempt.CancelPendingObservation = false
		if result.err != nil {
			return result.err.Error()
		}
		observation := result.observation
		if observation.Run != attempt.Run {
			return "workflow: observation identifies a different run"
		}
		if attempt.Evidence != nil && observation.ObservedAt.Before(attempt.Evidence.ObservedAt) {
			return "workflow: provider returned an older observation"
		}
		evidence, err := verify.Judge(observation)
		if err != nil {
			return err.Error()
		}
		attempt.Evidence = &evidence
		if observation.State != record.AttemptRunning {
			finishAttempt(work, job, attempt, evidence, observation.Detail, now)
		}
		return ""
	}
	return "workflow: invalid attempt action"
}

// recordSubmission validates run identity before retaining response resources.
// It defaults to uncertainty, then adopts admission, a capacity refusal, or an
// unsupported outcome only when the response is consistent. Valid handles may
// be retained despite a call error so recovery does not lose cleanup obligations.
// The caller must hold a state transaction and have validated the attempt claim.
func recordSubmission(work *execution, job *record.Job, attempt *record.Attempt, submission verify.Submission, callErr error, now time.Time) string {
	attempt.State = record.AttemptUncertain
	if run := submission.Run; run != (record.ProviderRun{}) {
		if run.Provider != attempt.Spec.Config.Provider || run.RequestID != attempt.SubmissionID || !validToken(run.RunID) {
			return "workflow: submission response identifies a different or invalid run"
		}
		if attempt.Run != (record.ProviderRun{}) && attempt.Run != run {
			return "workflow: provider changed the admitted run identity"
		}
	}
	resourceErr := recordResources(work, *attempt, submission.Resources)
	if callErr != nil {
		return callErr.Error()
	}
	if resourceErr != nil {
		return resourceErr.Error()
	}
	switch submission.State {
	case verify.Admitted:
		run := submission.Run
		if run == (record.ProviderRun{}) {
			return "workflow: admission does not identify the requested run"
		}
		attempt.Run, attempt.State = run, record.AttemptRunning
		work.Submission.RunID = run.RunID
		if work.Submission.AdmittedAt == nil {
			work.Submission.AdmittedAt = &now
		}
		if job.AdmittedAt == nil {
			job.AdmittedAt = &now
		}
		dispositionResources(work, attempt.ID, record.ResourceActive)
		return ""
	case verify.AtCapacity, verify.Unsupported:
		if submission.Run != (record.ProviderRun{}) || hasResources(work, attempt.ID) {
			return "workflow: non-admission response has external effects; reconciling submission"
		}
		if submission.State == verify.AtCapacity {
			attempt.State = record.AttemptQueued
			return ""
		}
		finishAttempt(work, job, attempt, record.Evidence{Verdict: record.VerdictUnsupported, ObservedAt: now}, submission.Detail, now)
		return "workflow: provider rejected the build as unsupported"
	case verify.SubmissionUncertain:
		return "workflow: submission outcome is uncertain: " + submission.Detail
	default:
		return "workflow: invalid provider submission state"
	}
}

// finishAttempt records a terminal verdict and its job outcome in the caller's
// transaction. Passed and canceled attempts request release; other outcomes
// retain their resources for diagnosis. Actual release happens separately.
func finishAttempt(work *execution, job *record.Job, attempt *record.Attempt, evidence record.Evidence, detail string, now time.Time) {
	attempt.Evidence, attempt.State, attempt.Claim, attempt.RetryAt = &evidence, record.AttemptFinished, nil, nil
	resourceState := record.ResourceRetained
	switch evidence.Verdict {
	case record.VerdictPassed:
		job.State, resourceState = record.JobCompleted, record.ResourceReleaseRequested
	case record.VerdictFailed:
		job.State = record.JobFailed
	case record.VerdictCanceled:
		job.State, attempt.State, resourceState = record.JobCanceled, record.AttemptCanceled, record.ResourceReleaseRequested
	default:
		job.State = record.JobNeedsAttention
	}
	job.FinishedAt, job.Detail = &now, detail
	dispositionResources(work, attempt.ID, resourceState)
	work.Attempt, work.Job = *attempt, *job
}
