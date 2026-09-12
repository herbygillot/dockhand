package workflow

import (
	"context"
	"reflect"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
)

// execution contains only the records needed for one job transition. It is
// private to workflow; persistence still reads and writes individual records.
type execution struct {
	Problem        string
	Job            record.Job
	Revision       record.Revision
	Plan           *record.VerificationPlan
	Attempt        record.Attempt
	Submission     record.Submission
	NextSubmission *record.Submission
	Resources      map[record.ResourceID]record.Resource
}

func loadExecution(ctx context.Context, r state.Reader, id record.JobID) (execution, error) {
	work := execution{Resources: map[record.ResourceID]record.Resource{}}
	var err error
	if work.Job, err = r.Job(ctx, id); err != nil {
		return work, err
	}
	if work.Job.Spec.InputRevision != "" {
		if work.Revision, err = r.Revision(ctx, work.Job.Spec.InputRevision); err != nil {
			return work, err
		}
	}
	attempts, err := r.AttemptsForJob(ctx, id)
	if err != nil {
		return work, err
	}
	if len(attempts) > 1 {
		work.Problem = "workflow: single-target cycle cannot select multiple attempts"
		return work, nil
	}
	if len(attempts) == 1 {
		work.Attempt = attempts[0]
		if work.Attempt.SubmissionID != "" {
			if work.Submission, err = r.Submission(ctx, work.Attempt.SubmissionID); err != nil {
				return work, err
			}
		}
		resources, err := r.ResourcesForAttempt(ctx, work.Attempt.ID)
		if err != nil {
			return work, err
		}
		for _, resource := range resources {
			work.Resources[resource.ID] = resource
		}
	}
	return work, nil
}
func (e *Engine) updateExecution(ctx context.Context, id record.JobID, fn func(*execution) error) error {
	return e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		before, err := loadExecution(ctx, tx, id)
		if err != nil {
			return err
		}
		work := before
		work.Resources = map[record.ResourceID]record.Resource{}
		for id, v := range before.Resources {
			work.Resources[id] = v
		}
		if err = fn(&work); err != nil {
			return err
		}
		if !reflect.DeepEqual(before.Job, work.Job) {
			if err = tx.PutJob(ctx, work.Job); err != nil {
				return err
			}
		}
		if work.Plan != nil {
			if err = tx.PutPlan(ctx, *work.Plan); err != nil {
				return err
			}
		}
		if before.Attempt.ID == "" && work.Attempt.ID != "" {
			if err = tx.PutAttempt(ctx, work.Attempt); err != nil {
				return err
			}
		}
		if !reflect.DeepEqual(before.Submission, work.Submission) {
			if err = tx.PutSubmission(ctx, work.Submission); err != nil {
				return err
			}
		}
		if work.NextSubmission != nil {
			if err = tx.PutSubmission(ctx, *work.NextSubmission); err != nil {
				return err
			}
		}
		if before.Attempt.ID != "" && !reflect.DeepEqual(before.Attempt, work.Attempt) {
			if err = tx.PutAttempt(ctx, work.Attempt); err != nil {
				return err
			}
		}
		for id, v := range work.Resources {
			if !reflect.DeepEqual(before.Resources[id], v) {
				if err = tx.PutResource(ctx, v); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
