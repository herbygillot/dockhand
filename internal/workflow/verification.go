package workflow

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/ledger"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/verify"
)

type attemptAction string

const (
	submitAttempt    attemptAction = "submit"
	reconcileAttempt attemptAction = "reconcile"
	observeAttempt   attemptAction = "observe"
	cancelAttempt    attemptAction = "cancel"
)

type attemptResult struct {
	submission     verify.Submission
	reconciliation verify.Reconciliation
	observation    verify.Observation
	err            error
}

func (c *cycle) advanceJob(ctx context.Context, id record.JobID) (bool, string, error) {
	e := c.engine
	var attempt record.Attempt
	var action attemptAction
	var changed bool
	var detail string
	err := e.Ledger.Update(ctx, func(_ context.Context, tx *ledger.Transaction) error {
		job, ok := tx.State.Jobs[id]
		if !ok {
			return fmt.Errorf("%w: job %s", ErrNotFound, id)
		}
		if jobTerminal(job.State) {
			return nil
		}
		now := e.now()
		for _, candidate := range tx.State.Attempts {
			if candidate.JobID == id {
				if attempt.ID != "" {
					detail = "workflow: this cycle cannot execute multiple attempts for one job"
					return nil
				}
				attempt = candidate
			}
		}
		if job.CancelRequestedAt != nil && (attempt.ID == "" || attempt.State == record.AttemptQueued) {
			if live(attempt.Claim, now) {
				return nil
			}
			if attempt.ID != "" {
				if attempt.Run != (record.ProviderRun{}) || hasResources(tx.State, attempt.ID) {
					return fmt.Errorf("%w: queued attempt has external effects", ledger.ErrInvalidState)
				}
				finishAttempt(&tx.State, &job, &attempt, record.Evidence{Verdict: record.VerdictCanceled, ObservedAt: now}, "Canceled before admission", now)
			} else {
				job.State, job.FinishedAt, job.Detail = record.JobCanceled, &now, "Canceled before admission"
				tx.State.Jobs[id] = job
			}
			changed = true
			return nil
		}
		if job.Spec.Action != record.Verify {
			detail = ErrNotImplemented.Error()
			return nil
		}
		if attempt.ID == "" {
			plan, build, err := verify.PlanSingle(job, tx.State.Revisions[job.Spec.InputRevision])
			if err != nil {
				job.State, job.FinishedAt, job.Detail = record.JobNeedsAttention, &now, err.Error()
				tx.State.Jobs[id] = job
				changed, detail = true, err.Error()
				return nil
			}
			attempt = record.Attempt{ID: record.AttemptID("attempt_" + rand.Text()), JobID: id, TargetID: plan.Targets[0].ID, Spec: build, State: record.AttemptQueued, CreatedAt: now}
			attempt.SubmissionID = record.RequestID("submit_" + string(attempt.ID))
			tx.State.Plans[id] = plan
			tx.State.Attempts[attempt.ID] = attempt
			job.State = record.JobActive
			tx.State.Jobs[id] = job
			changed = true
		}
		if attemptTerminal(attempt.State) {
			return fmt.Errorf("%w: terminal attempt belongs to active job", ledger.ErrInvalidState)
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
			return fmt.Errorf("%w: unsupported attempt state %q", ledger.ErrInvalidState, attempt.State)
		}
		if attempt.SubmissionID == "" {
			return fmt.Errorf("%w: attempt has no submission identity", ledger.ErrInvalidState)
		}
		if err := c.providerReady(attempt.Spec.Config, action == submitAttempt); err != nil {
			detail = err.Error()
			retry := now.Add(c.retry)
			attempt.LastError, attempt.RetryAt = detail, &retry
			tx.State.Attempts[attempt.ID] = attempt
			changed, action = true, ""
			return nil
		}
		claim, err := c.claim(&attempt.ClaimGeneration, now)
		if err != nil {
			return err
		}
		attempt.Claim = claim
		if action == submitAttempt {
			attempt.State = record.AttemptSubmitting
		}
		tx.State.Attempts[attempt.ID] = attempt
		changed = true
		return nil
	})
	if err != nil {
		return false, detail, err
	}
	if action == "" {
		return changed, detail, nil
	}
	response := c.callAttempt(ctx, action, attempt)
	err = e.Ledger.Update(ctx, func(_ context.Context, tx *ledger.Transaction) error {
		current, ok := tx.State.Attempts[attempt.ID]
		now := e.now()
		if !ok || !owns(current.Claim, attempt.Claim, now) {
			return ErrClaimLost
		}
		job, ok := tx.State.Jobs[id]
		if !ok || jobTerminal(job.State) || current.State != attempt.State {
			return ErrClaimLost
		}
		current.Claim = nil
		current.LastError, current.RetryAt = "", nil
		detail = c.recordAttempt(&tx.State, &job, &current, action, response, now)
		if !attemptTerminal(current.State) {
			retry := now.Add(c.retry)
			current.RetryAt = &retry
		}
		current.LastError = detail
		tx.State.Attempts[current.ID] = current
		tx.State.Jobs[id] = job
		return nil
	})
	return changed, detail, err
}

func (c *cycle) callAttempt(ctx context.Context, action attemptAction, attempt record.Attempt) attemptResult {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
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

func (c *cycle) recordAttempt(state *ledger.State, job *record.Job, attempt *record.Attempt, action attemptAction, result attemptResult, now time.Time) string {
	switch action {
	case submitAttempt:
		return recordSubmission(state, job, attempt, result.submission, result.err, now)
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
			return recordSubmission(state, job, attempt, submission, nil, now)
		case verify.RequestClosed:
			if result.reconciliation.Submission.Run != (record.ProviderRun{}) || attempt.Run != (record.ProviderRun{}) {
				return "workflow: closed submission unexpectedly identifies a run"
			}
			if err := recordResources(state, *attempt, result.reconciliation.Submission.Resources); err != nil {
				return err.Error()
			}
			attempt.ClosedSubmissions = append(attempt.ClosedSubmissions, attempt.SubmissionID)
			if hasResources(*state, attempt.ID) {
				finishAttempt(state, job, attempt, record.Evidence{Verdict: record.VerdictErrored, ObservedAt: now}, "Submission closed after partial provisioning", now)
				dispositionResources(state, attempt.ID, record.ResourceReleaseRequested)
				return job.Detail
			}
			if job.CancelRequestedAt != nil {
				finishAttempt(state, job, attempt, record.Evidence{Verdict: record.VerdictCanceled, ObservedAt: now}, "Canceled before admission", now)
			} else {
				attempt.SubmissionID = record.RequestID("submit_" + rand.Text())
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
			finishAttempt(state, job, attempt, evidence, observation.Detail, now)
		}
		return ""
	}
	return "workflow: invalid attempt action"
}

func recordSubmission(state *ledger.State, job *record.Job, attempt *record.Attempt, submission verify.Submission, callErr error, now time.Time) string {
	attempt.State = record.AttemptUncertain
	if run := submission.Run; run != (record.ProviderRun{}) {
		if run.Provider != attempt.Spec.Config.Provider || run.RequestID != attempt.SubmissionID || !validToken(run.RunID) {
			return "workflow: submission response identifies a different or invalid run"
		}
		if attempt.Run != (record.ProviderRun{}) && attempt.Run != run {
			return "workflow: provider changed the admitted run identity"
		}
	}
	resourceErr := recordResources(state, *attempt, submission.Resources)
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
		if job.AdmittedAt == nil {
			job.AdmittedAt = &now
		}
		dispositionResources(state, attempt.ID, record.ResourceActive)
		return ""
	case verify.AtCapacity, verify.Unsupported:
		if submission.Run != (record.ProviderRun{}) || hasResources(*state, attempt.ID) {
			return "workflow: non-admission response has external effects; reconciling submission"
		}
		if submission.State == verify.AtCapacity {
			attempt.State = record.AttemptQueued
			return ""
		}
		finishAttempt(state, job, attempt, record.Evidence{Verdict: record.VerdictUnsupported, ObservedAt: now}, submission.Detail, now)
		return "workflow: provider rejected the build as unsupported"
	case verify.SubmissionUncertain:
		return "workflow: submission outcome is uncertain: " + submission.Detail
	default:
		return "workflow: invalid provider submission state"
	}
}

func finishAttempt(state *ledger.State, job *record.Job, attempt *record.Attempt, evidence record.Evidence, detail string, now time.Time) {
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
	dispositionResources(state, attempt.ID, resourceState)
	state.Attempts[attempt.ID], state.Jobs[job.ID] = *attempt, *job
}
