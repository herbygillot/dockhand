package workflow

import (
	"context"
	"crypto/rand"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

// attemptAction selects one provider operation after an attempt has been claimed.
type attemptAction string

const (
	submitAttempt    attemptAction = "submit"
	reconcileAttempt attemptAction = "reconcile"
	observeAttempt   attemptAction = "observe"
	cancelAttempt    attemptAction = "cancel"
)

// attemptResult carries one provider response back across the transaction boundary.
type attemptResult struct {
	submission     verify.Submission
	reconciliation verify.Reconciliation
	observation    verify.Observation
	err            error
}

// advanceJob persists planning or performs one claimed provider action for a job.
// Provider calls occur outside the state transaction.
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
			if job.State.Terminal() {
				return nil
			}
			now := e.now()
			if job.Claim.Live(now) || !due(job.RetryAt, now) {
				return nil
			}
			if job.CancelRequestedAt != nil && len(work.Attempts) == 0 {
				job.State, job.FinishedAt, job.Detail = record.JobCanceled, &now, "Canceled before admission"
				work.Job = job
				changed = true
				return nil
			}
			if job.Phase != record.PhaseVerification {
				detail = ErrNotImplemented.Error()
				job.State, job.FinishedAt, job.Detail = record.JobNeedsAttention, &now, detail
				work.Job = job
				changed = true
				return nil
			}
			if len(work.Attempts) == 0 {
				var plan record.VerificationPlan
				var builds []record.BuildSpec
				var reused record.Attempt
				var explanation string
				if job.Spec.Build == nil && job.Spec.BuildRequirements != nil {
					reused, plan, explanation, err = selectRecordedVerification(ctx, tx, job, work.Revision)
					if err != nil {
						return err
					}
					if reused.ID == "" {
						_, _, planningErr := verify.Plan(job, work.Revision)
						detail = explanation
						if planningErr != nil {
							detail += "; " + planningErr.Error()
						}
						job.ReuseDetail = explanation
						job.State, job.FinishedAt, job.Detail = record.JobNeedsAttention, &now, detail
						work.Job = job
						changed = true
						return nil
					}
				} else {
					plan, builds, err = verify.Plan(job, work.Revision)
					if err != nil {
						job.State, job.FinishedAt, job.Detail = record.JobNeedsAttention, &now, err.Error()
						work.Job = job
						changed, detail = true, err.Error()
						return nil
					}
					if len(builds) == 1 {
						reused, explanation, err = selectVerification(ctx, tx, job, builds[0])
						if err != nil {
							return err
						}
					}
				}
				job.ReuseDetail = explanation
				if reused.ID != "" {
					job.ReusedAttempt = reused.ID
					job.State, job.FinishedAt, job.Detail = record.JobCompleted, &now, explanation
					if job.Spec.PublishTo != nil {
						job.Phase = record.PhasePublication
						job.State, job.FinishedAt, job.Detail = record.JobActive, nil, explanation+"; publication pending"
					}
					work.Job, work.Plan = job, &plan
					changed = true
					return nil
				}
				for i, build := range builds {
					candidate := record.Attempt{ID: record.AttemptID("attempt_" + rand.Text()), JobID: id, TargetID: plan.Targets[i].ID, Spec: build, State: record.AttemptQueued, CreatedAt: now}
					candidate.SubmissionID = record.RequestID("submit_" + string(candidate.ID))
					work.Attempts[candidate.ID] = candidate
					work.Submissions[candidate.SubmissionID] = record.Submission{ID: candidate.SubmissionID, AttemptID: candidate.ID, Sequence: 1, Provider: build.Config.Provider, CreatedAt: now}
				}
				work.Plan = &plan
				job.State = record.JobActive
				work.Job = job
				changed = true
			}

			var ok bool
			attempt, ok = selectAttempt(work, now, job.CancelRequestedAt != nil)
			if !ok {
				return nil
			}
			if job.CancelRequestedAt != nil && attempt.State == record.AttemptQueued {
				if attempt.Run != (record.ProviderRun{}) || hasResources(work, attempt.ID) {
					return fmt.Errorf("%w: queued attempt has external effects", state.ErrInvalid)
				}
				finishAttempt(work, &job, &attempt, record.Evidence{Verdict: record.VerdictCanceled, ObservedAt: now}, "Canceled before admission", now)
				work.Job = job
				changed = true
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
			if !c.providerChecked || c.providerName != attempt.Spec.Config.Provider {
				needsProvider, action = true, ""
				return nil
			}
			if err := c.providerReady(attempt.Spec.Config, action == submitAttempt); err != nil {
				detail = err.Error()
				retry := now.Add(c.retry)
				attempt.LastError, attempt.RetryAt = detail, &retry
				work.Attempts[attempt.ID] = attempt
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
			work.Attempts[attempt.ID] = attempt
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
		c.checkProvider(ctx, attempt.Spec.Config.Provider)
	}
	if action == "" {
		return changed, detail, nil
	}
	response := c.callAttempt(ctx, action, attempt)
	err = e.updateExecution(ctx, id, func(tx state.Tx, work *execution) error {
		current, exists := work.Attempts[attempt.ID]
		now := e.now()
		if !exists || !current.Claim.Owns(attempt.Claim, now) {
			return ErrClaimLost
		}
		job := work.Job
		if job.State.Terminal() || current.State != attempt.State {
			return ErrClaimLost
		}
		current.Claim = nil
		current.RetryAt = nil
		detail = c.recordAttempt(work, &job, &current, action, response, now)
		if !current.State.Terminal() {
			delay := c.retry
			if current.State == record.AttemptRunning && detail == "" && job.CancelRequestedAt == nil {
				delay = c.observe
			}
			retry := now.Add(delay)
			current.RetryAt = &retry
		}
		current.LastError = detail
		work.Attempts[current.ID] = current
		work.Job = job
		return nil
	})
	return changed, detail, err
}

// selectAttempt chooses one due, unclaimed attempt. Ordering by due time lets new
// targets fill available capacity before routine observations become due.
func selectAttempt(work *execution, now time.Time, canceling bool) (record.Attempt, bool) {
	ids := make([]string, 0, len(work.Attempts))
	for id, attempt := range work.Attempts {
		if attempt.State.Terminal() || attempt.Claim.Live(now) || !due(attempt.RetryAt, now) {
			continue
		}
		ids = append(ids, string(id))
	}
	sort.Slice(ids, func(i, j int) bool {
		a := work.Attempts[record.AttemptID(ids[i])]
		b := work.Attempts[record.AttemptID(ids[j])]
		if canceling {
			aQueued, bQueued := a.State == record.AttemptQueued, b.State == record.AttemptQueued
			if aQueued != bQueued {
				return aQueued
			}
		}
		aDue, bDue := time.Time{}, time.Time{}
		if a.RetryAt != nil {
			aDue = *a.RetryAt
		}
		if b.RetryAt != nil {
			bDue = *b.RetryAt
		}
		if compared := aDue.Compare(bDue); compared != 0 {
			return compared < 0
		}
		return ids[i] < ids[j]
	})
	if len(ids) == 0 {
		return record.Attempt{}, false
	}
	return work.Attempts[record.AttemptID(ids[0])], true
}

// callAttempt performs exactly one provider operation outside the write transaction.
func (c *cycle) callAttempt(ctx context.Context, action attemptAction, attempt record.Attempt) attemptResult {
	ctx, cancel := context.WithTimeout(ctx, c.attemptTimeout(action))
	defer cancel()
	var result attemptResult
	switch action {
	case submitAttempt:
		result.submission, result.err = c.provider.Submit(ctx, verify.Request{ID: attempt.SubmissionID, AttemptID: attempt.ID, Spec: attempt.Spec})
	case reconcileAttempt:
		result.reconciliation, result.err = c.provider.Reconcile(ctx, attempt.SubmissionID)
	case observeAttempt:
		result.observation, result.err = c.provider.Observe(ctx, attempt.Run)
	case cancelAttempt:
		result.err = c.provider.Cancel(ctx, attempt.Run)
	}
	if result.err == nil {
		result.err = ctx.Err()
	}
	return result
}

// recordAttempt applies a provider response after the caller revalidates its claim.
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
			if result.reconciliation.Submission.Run != (record.ProviderRun{}) || attempt.Run != (record.ProviderRun{}) {
				return "workflow: closed submission unexpectedly identifies a run"
			}
			if err := recordResources(work, *attempt, result.reconciliation.Submission.Resources); err != nil {
				return err.Error()
			}
			current := work.Submissions[attempt.SubmissionID]
			current.ClosedAt = &now
			work.Submissions[current.ID] = current
			if hasResources(work, attempt.ID) {
				detail := "Submission closed after partial provisioning"
				if attempt.LastError != "" {
					detail += ": " + attempt.LastError
				}
				finishAttempt(work, job, attempt, record.Evidence{Verdict: record.VerdictErrored, ObservedAt: now}, detail, now)
				dispositionResources(work, attempt.ID, record.ResourceReleaseRequested)
				return job.Detail
			}
			if job.CancelRequestedAt != nil {
				finishAttempt(work, job, attempt, record.Evidence{Verdict: record.VerdictCanceled, ObservedAt: now}, "Canceled before admission", now)
			} else {
				attempt.SubmissionID = record.RequestID("submit_" + rand.Text())
				work.Submissions[attempt.SubmissionID] = record.Submission{ID: attempt.SubmissionID, AttemptID: attempt.ID, Sequence: current.Sequence + 1, Provider: attempt.Spec.Config.Provider, CreatedAt: now}
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
			finishAttempt(work, job, attempt, evidence, observation.Detail, now)
		}
		return ""
	}
	return "workflow: invalid attempt action"
}

// recordSubmission validates a submission response and retains its recoverable identity.
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
		current := work.Submissions[attempt.SubmissionID]
		current.RunID = run.RunID
		if current.AdmittedAt == nil {
			current.AdmittedAt = &now
		}
		work.Submissions[current.ID] = current
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

// finishAttempt records one terminal result and then reevaluates the whole job.
func finishAttempt(work *execution, job *record.Job, attempt *record.Attempt, evidence record.Evidence, detail string, now time.Time) {
	attempt.Evidence, attempt.State, attempt.Claim, attempt.RetryAt = &evidence, record.AttemptFinished, nil, nil
	resourceState := record.ResourceRetained
	if evidence.Verdict == record.VerdictPassed || evidence.Verdict == record.VerdictCanceled {
		resourceState = record.ResourceReleaseRequested
	}
	if evidence.Verdict == record.VerdictCanceled {
		attempt.State = record.AttemptCanceled
	}
	dispositionResources(work, attempt.ID, resourceState)
	work.Attempts[attempt.ID] = *attempt
	settleVerification(work, job, detail, now)
}

// settleVerification leaves partial cohorts active and derives a terminal job outcome
// only after every planned attempt has conclusive evidence.
func settleVerification(work *execution, job *record.Job, detail string, now time.Time) {
	counts := map[record.Verdict]int{}
	for _, attempt := range work.Attempts {
		if !attempt.State.Terminal() || attempt.Evidence == nil {
			job.State, job.FinishedAt = record.JobActive, nil
			return
		}
		counts[attempt.Evidence.Verdict]++
	}
	if len(work.Attempts) == 0 {
		return
	}
	switch {
	case counts[record.VerdictFailed] > 0:
		job.State = record.JobFailed
	case counts[record.VerdictErrored]+counts[record.VerdictBlocked]+counts[record.VerdictUnsupported]+counts[record.VerdictUnknown] > 0:
		job.State = record.JobNeedsAttention
	case counts[record.VerdictCanceled] > 0:
		job.State = record.JobCanceled
	default:
		job.State = record.JobCompleted
	}
	job.FinishedAt = &now
	if len(work.Attempts) == 1 {
		job.Detail = detail
	} else {
		parts := make([]string, 0, 5)
		for _, verdict := range []record.Verdict{record.VerdictPassed, record.VerdictFailed, record.VerdictBlocked, record.VerdictUnsupported, record.VerdictErrored, record.VerdictCanceled} {
			if counts[verdict] > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", counts[verdict], verdict))
			}
		}
		job.Detail = "Verification completed: " + strings.Join(parts, ", ")
	}
	if job.State == record.JobCompleted && job.Spec.PublishTo != nil {
		job.Phase = record.PhasePublication
		job.State, job.FinishedAt, job.Detail = record.JobActive, nil, "Verification passed; publication pending"
	}
}
