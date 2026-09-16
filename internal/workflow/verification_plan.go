package workflow

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

// initializeVerification records planning or reuse in the caller's transaction.
func initializeVerification(ctx context.Context, tx state.Reader, work *execution, now time.Time) (bool, string, error) {
	job := work.Job
	id := job.ID
	var err error
	var detail string

	var plan record.VerificationPlan
	var builds []record.BuildSpec
	var reused record.Attempt
	var explanation string
	if job.Spec.IncludeDependents {
		if work.Plan == nil {
			return true, "", fmt.Errorf("workflow: dependent coverage was not recorded")
		}
		for _, target := range work.Plan.Targets {
			if target.Problem != "" || target.Build == nil {
				continue
			}
			build := *target.Build
			candidate := record.Attempt{ID: record.AttemptID("attempt_" + rand.Text()), JobID: job.ID, TargetID: target.ID, Spec: build, State: record.AttemptQueued, CreatedAt: now}
			candidate.SubmissionID = record.RequestID("submit_" + string(candidate.ID))
			work.Attempts[candidate.ID] = candidate
			work.Submissions[candidate.SubmissionID] = record.Submission{ID: candidate.SubmissionID, AttemptID: candidate.ID, Sequence: 1, Provider: build.Config.Provider, CreatedAt: now}
		}
		job.State = record.JobActive
		if len(work.Attempts) == 0 {
			settleVerification(work, &job, "", now)
		}
		work.Job = job
		return len(work.Attempts) == 0, job.Detail, nil
	}
	if job.Spec.Build == nil && job.Spec.BuildRequirements != nil {
		reused, plan, explanation, err = selectRecordedVerification(ctx, tx, job, work.Revision)
		if err != nil {
			return true, "", err
		}
		if reused.ID == "" {
			_, _, planningErr := verify.Plan(job, work.Revision)
			detail = explanation
			if planningErr != nil {
				detail += "; " + planningErr.Error()
			}
			job.ReuseDetail = explanation
			finishJob(&job, record.JobNeedsAttention, detail, now)
			work.Job = job

			return true, detail, nil
		}
	} else {
		plan, builds, err = verify.Plan(job, work.Revision)
		if err != nil {
			finishJob(&job, record.JobNeedsAttention, err.Error(), now)
			work.Job = job
			detail = err.Error()
			return true, detail, nil
		}
		if len(builds) == 1 {
			reused, explanation, err = selectVerification(ctx, tx, job, builds[0])
			if err != nil {
				return true, "", err
			}
		}
	}
	job.ReuseDetail = explanation
	if reused.ID != "" {
		job.ReusedAttempt = reused.ID
		finishJob(&job, record.JobCompleted, explanation, now)
		if job.Spec.PublishTo != nil {
			job.Phase = record.PhasePublication
			job.State, job.FinishedAt, job.Detail = record.JobActive, nil, explanation+"; publication pending"
		}
		work.Job, work.Plan = job, &plan

		return true, detail, nil
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

	return false, "", nil
}
