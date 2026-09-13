package workflow

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
)

type JobStatus struct {
	Job          record.Job
	Attempts     []record.Attempt
	Publications []record.PublicationAction
	// Reused is the original execution cited by a completed job, not a new attempt.
	Reused *record.Attempt `json:",omitempty"`
}
type Status struct {
	Repository   record.RepositoryID
	ReadAt       time.Time
	Jobs         []JobStatus
	Changes      []record.Change
	Revisions    []record.Revision
	PullRequests []record.PullRequest
	Resources    []record.Resource
}

func EmptyStatus(at time.Time) Status {
	return Status{ReadAt: at.UTC(), Jobs: []JobStatus{}, Changes: []record.Change{}, Revisions: []record.Revision{}, PullRequests: []record.PullRequest{}, Resources: []record.Resource{}}
}
func validateScope(scope Scope) error {
	if scope.All == (len(scope.Jobs) > 0) {
		return ErrInvalidScope
	}
	for _, id := range scope.Jobs {
		if id == "" {
			return ErrInvalidScope
		}
	}
	return nil
}
func (e *Engine) checkScope(scope Scope) error {
	if err := validateScope(scope); err != nil {
		return err
	}
	if e == nil || e.State == nil || e.Repository == "" {
		return ErrNoState
	}
	return nil
}
func checkJobs(ctx context.Context, r state.Reader, scope Scope) error {
	for _, id := range scope.Jobs {
		if _, err := r.Job(ctx, id); err != nil {
			return fmt.Errorf("job %s: %w", id, err)
		}
	}
	return nil
}
func collect[T any](ctx context.Context, q state.Query, get func(context.Context, state.Query) ([]T, error), id func(T) string) ([]T, error) {
	result := []T{}
	q.Limit = 256
	for {
		batch, err := get(ctx, q)
		if err != nil {
			return nil, err
		}
		result = append(result, batch...)
		if len(batch) < q.Limit {
			return result, nil
		}
		q.After = id(batch[len(batch)-1])
	}
}
func (e *Engine) Status(ctx context.Context, scope Scope) (Status, error) {
	if err := e.checkScope(scope); err != nil {
		return Status{}, err
	}
	result := EmptyStatus(e.now())
	result.Repository = e.Repository
	err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		if err := checkJobs(ctx, r, scope); err != nil {
			return err
		}
		selection := scope.Jobs
		if scope.All {
			selection = nil
		}
		q := state.Query{Jobs: selection}
		jobs, err := collect(ctx, q, r.Jobs, func(v record.Job) string { return string(v.ID) })
		if err != nil {
			return err
		}
		for _, job := range jobs {
			attempts, err := r.AttemptsForJob(ctx, job.ID)
			if err != nil {
				return err
			}
			entry := JobStatus{Job: job, Attempts: attempts, Publications: []record.PublicationAction{}}
			if job.ReusedAttempt != "" {
				original, err := r.Attempt(ctx, job.ReusedAttempt)
				if err != nil {
					return err
				}
				entry.Reused = &original
			}
			result.Jobs = append(result.Jobs, entry)
		}
		if result.Changes, err = collect(ctx, q, r.Changes, func(v record.Change) string { return string(v.ID) }); err != nil {
			return err
		}
		if result.Revisions, err = collect(ctx, q, r.Revisions, func(v record.Revision) string { return string(v.ID) }); err != nil {
			return err
		}
		result.Resources, err = collect(ctx, q, r.Resources, func(v record.Resource) string { return string(v.ID) })
		return err
	})
	if err != nil {
		return Status{}, err
	}
	slices.SortFunc(result.Jobs, func(a, b JobStatus) int {
		if cmp := a.Job.AcceptedAt.Compare(b.Job.AcceptedAt); cmp != 0 {
			return cmp
		}
		return strings.Compare(string(a.Job.ID), string(b.Job.ID))
	})
	return result, nil
}
