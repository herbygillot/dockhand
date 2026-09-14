package workflow

import (
	"context"
	"reflect"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// execution contains only the records needed for one job transition. It is
// private to workflow; persistence still reads and writes individual records.
type execution struct {
	Job         record.Job
	Revision    record.Revision
	Plan        *record.VerificationPlan
	Attempts    map[record.AttemptID]record.Attempt
	Submissions map[record.RequestID]record.Submission
	Resources   map[record.ResourceID]record.Resource
}

func loadExecution(ctx context.Context, r state.Reader, id record.JobID) (execution, error) {
	work := execution{
		Attempts:    map[record.AttemptID]record.Attempt{},
		Submissions: map[record.RequestID]record.Submission{},
		Resources:   map[record.ResourceID]record.Resource{},
	}
	var err error
	if work.Job, err = r.Job(ctx, id); err != nil {
		return work, err
	}
	revisionID := work.Job.ResultRevision
	if revisionID == "" {
		revisionID = work.Job.Spec.InputRevision
	}
	if revisionID != "" {
		if work.Revision, err = r.Revision(ctx, revisionID); err != nil {
			return work, err
		}
	}
	attempts, err := r.AttemptsForJob(ctx, id)
	if err != nil {
		return work, err
	}
	for _, attempt := range attempts {
		work.Attempts[attempt.ID] = attempt
		if attempt.SubmissionID != "" {
			submission, err := r.Submission(ctx, attempt.SubmissionID)
			if err != nil {
				return work, err
			}
			work.Submissions[submission.ID] = submission
		}
		resources, err := r.ResourcesForAttempt(ctx, attempt.ID)
		if err != nil {
			return work, err
		}
		for _, resource := range resources {
			work.Resources[resource.ID] = resource
		}
	}
	return work, nil
}

func cloneExecution(before execution) execution {
	work := before
	work.Attempts = make(map[record.AttemptID]record.Attempt, len(before.Attempts))
	for id, value := range before.Attempts {
		work.Attempts[id] = value
	}
	work.Submissions = make(map[record.RequestID]record.Submission, len(before.Submissions))
	for id, value := range before.Submissions {
		work.Submissions[id] = value
	}
	work.Resources = make(map[record.ResourceID]record.Resource, len(before.Resources))
	for id, value := range before.Resources {
		work.Resources[id] = value
	}
	return work
}

func (e *Engine) updateExecution(ctx context.Context, id record.JobID, fn func(state.Tx, *execution) error) error {
	return e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		before, err := loadExecution(ctx, tx, id)
		if err != nil {
			return err
		}
		work := cloneExecution(before)
		if err = fn(tx, &work); err != nil {
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
		for id, value := range work.Attempts {
			if _, exists := before.Attempts[id]; !exists {
				if err = tx.PutAttempt(ctx, value); err != nil {
					return err
				}
			}
		}
		for id, value := range work.Submissions {
			if !reflect.DeepEqual(before.Submissions[id], value) {
				if err = tx.PutSubmission(ctx, value); err != nil {
					return err
				}
			}
		}
		for id, value := range work.Attempts {
			if old, exists := before.Attempts[id]; exists && !reflect.DeepEqual(old, value) {
				if err = tx.PutAttempt(ctx, value); err != nil {
					return err
				}
			}
		}
		for id, value := range work.Resources {
			if !reflect.DeepEqual(before.Resources[id], value) {
				if err = tx.PutResource(ctx, value); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
