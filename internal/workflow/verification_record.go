package workflow

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

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
				detail := result.reconciliation.Submission.Detail
				if detail == "" {
					detail = "Canceled before admission"
				}
				finishAttempt(work, job, attempt, record.Evidence{Verdict: record.VerdictCanceled, ObservedAt: now}, detail, now)
			} else if result.reconciliation.Submission.State == verify.Unsupported {
				finishAttempt(work, job, attempt, record.Evidence{Verdict: record.VerdictUnsupported, ObservedAt: now}, result.reconciliation.Submission.Detail, now)
			} else {
				attempt.SubmissionID = record.RequestID("submit_" + rand.Text())
				work.Submissions[attempt.SubmissionID] = record.Submission{ID: attempt.SubmissionID, AttemptID: attempt.ID, Sequence: current.Sequence + 1, Provider: attempt.Spec.Config.Provider, CreatedAt: now}
				attempt.State = record.AttemptQueued
			}
			return ""
		case verify.RunUnknown:
			attempt.State = record.AttemptUncertain
			if detail := result.reconciliation.Submission.Detail; detail != "" {
				recordVerificationProgress(work, job, detail)
				return ""
			}
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
		recordVerificationProgress(work, job, observation.Detail)
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
		recordVerificationProgress(work, job, submission.Detail)
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
		if submission.Detail != "" {
			recordVerificationProgress(work, job, submission.Detail)
			return ""
		}
		return "workflow: submission outcome is uncertain"
	default:
		return "workflow: invalid provider submission state"
	}
}

// finishAttempt records one terminal result and then reevaluates the whole job.
func finishAttempt(work *execution, job *record.Job, attempt *record.Attempt, evidence record.Evidence, detail string, now time.Time) {
	attempt.Evidence, attempt.State, attempt.Claim, attempt.RetryAt = &evidence, record.AttemptFinished, nil, nil
	resourceState := record.ResourceRetained
	if !job.Spec.KeepFailed || evidence.Verdict == record.VerdictPassed || evidence.Verdict == record.VerdictCanceled {
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
	var coverageProblems []string
	if work.Plan != nil {
		coverageProblems = verify.CoverageProblems(*work.Plan)
	}
	if len(work.Attempts) == 0 && len(coverageProblems) == 0 {
		return
	}
	switch {
	case len(coverageProblems) > 0:
		job.State = record.JobNeedsAttention
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
	if len(coverageProblems) > 0 {
		job.Detail += "; incomplete coverage: " + strings.Join(coverageProblems, "; ")
	}
	if job.State == record.JobCompleted && job.Spec.PublishTo != nil {
		job.Phase = record.PhasePublication
		job.State, job.FinishedAt, job.Detail = record.JobActive, nil, "Verification passed; publication pending"
	}
}

func recordVerificationProgress(work *execution, job *record.Job, detail string) {
	if len(work.Attempts) == 1 && detail != "" {
		job.Detail = detail
	}
}
