package workflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// StatusFilter narrows a recorded snapshot without selecting work for execution.
// Active includes queued jobs and jobs waiting for capacity or a retry.
// Branch matches the contribution's recorded branch, including closed contributions.
type StatusFilter struct {
	Target   string          `json:",omitempty"`
	ChangeID record.ChangeID `json:",omitempty"`
	JobID    record.JobID    `json:",omitempty"`
	Branch   string          `json:",omitempty"`
	Active   bool            `json:",omitempty"`
}

func (f StatusFilter) Validate() error {
	if f.Branch != "" && f.ChangeID != "" || f.JobID != "" && (f.Branch != "" || f.Target != "" || f.ChangeID != "") {
		return fmt.Errorf("workflow: status accepts one of --job, --branch, or --change")
	}
	if f.Target != "" && !macports.ValidName(f.Target) || f.ChangeID != "" && !validToken(string(f.ChangeID)) {
		return ErrInvalidRequest
	}
	if f.JobID != "" && !validToken(string(f.JobID)) {
		return fmt.Errorf("workflow: invalid status job ID")
	}
	if f.Branch != "" && !git.ValidBranchName(f.Branch) {
		return fmt.Errorf("workflow: invalid status branch")
	}
	return nil
}

type JobStatus struct {
	Plan         *record.VerificationPlan `json:",omitempty"`
	Job          record.Job
	Attempts     []record.Attempt
	Publications []record.PublicationAction
	// Reused is the original execution cited by a completed job, not a new attempt.
	Reused *record.Attempt `json:",omitempty"`
}
type Status struct {
	Filter       *StatusFilter `json:",omitempty"`
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
	return e.status(ctx, scope, StatusFilter{})
}

func (e *Engine) FilteredStatus(ctx context.Context, filter StatusFilter) (Status, error) {
	if err := filter.Validate(); err != nil {
		return Status{}, err
	}
	scope := Scope{All: true}
	if filter.JobID != "" {
		scope = Scope{Jobs: []record.JobID{filter.JobID}}
	}
	return e.status(ctx, scope, filter)
}

func (e *Engine) status(ctx context.Context, scope Scope, filter StatusFilter) (Status, error) {
	if err := e.checkScope(scope); err != nil {
		return Status{}, err
	}
	result := EmptyStatus(e.now())
	result.Repository = e.Repository
	if filter != (StatusFilter{}) {
		result.Filter = &filter
	}
	err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		if err := checkJobs(ctx, r, scope); err != nil {
			return err
		}
		selection := scope.Jobs
		if scope.All {
			selection = nil
		}
		q := state.Query{Jobs: selection, Pending: filter.Active, Branch: filter.Branch, Target: filter.Target, ChangeID: filter.ChangeID}
		jobs, err := collect(ctx, q, r.Jobs, func(v record.Job) string { return string(v.ID) })
		if err != nil {
			return err
		}
		if filter.Active || filter.Branch != "" || filter.Target != "" || filter.ChangeID != "" {
			selection = make([]record.JobID, 0, len(jobs))
			for _, job := range jobs {
				selection = append(selection, job.ID)
			}
		}
		for _, job := range jobs {
			attempts, err := r.AttemptsForJob(ctx, job.ID)
			if err != nil {
				return err
			}
			entry := JobStatus{Job: job, Attempts: attempts, Publications: []record.PublicationAction{}}
			if job.Spec.IncludeDependents {
				plan, err := r.Plan(ctx, job.ID)
				if err != nil && !errors.Is(err, state.ErrNotFound) {
					return err
				}
				if err == nil {
					entry.Plan = &plan
				}
			}
			if job.Spec.Destination == record.Published {
				publication, err := r.PublicationForJob(ctx, job.ID)
				if err == nil {
					entry.Publications = append(entry.Publications, publication)
				} else if !errors.Is(err, state.ErrNotFound) {
					return err
				}
			}
			if job.ReusedAttempt != "" {
				original, err := r.Attempt(ctx, job.ReusedAttempt)
				if err != nil {
					return err
				}
				entry.Reused = &original
			}
			result.Jobs = append(result.Jobs, entry)
		}
		if result.Changes, err = collectRelated(ctx, selection, r.Changes, func(v record.Change) string { return string(v.ID) }); err != nil {
			return err
		}
		for _, change := range result.Changes {
			if change.PullRequestID != "" {
				pr, err := r.PullRequest(ctx, change.PullRequestID)
				if err != nil {
					return err
				}
				result.PullRequests = append(result.PullRequests, pr)
			}
		}
		if result.Revisions, err = collectRelated(ctx, selection, r.Revisions, func(v record.Revision) string { return string(v.ID) }); err != nil {
			return err
		}
		result.Resources, err = collectRelated(ctx, selection, r.Resources, func(v record.Resource) string { return string(v.ID) })
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

// Read related records in bounded batches so a large selection does not create
// an unbounded SQL parameter list. Shared contributions and revisions appear once.
func collectRelated[T any](ctx context.Context, jobs []record.JobID, get func(context.Context, state.Query) ([]T, error), id func(T) string) ([]T, error) {
	if jobs == nil {
		return collect(ctx, state.Query{}, get, id)
	}
	result := []T{}
	seen := make(map[string]bool)
	for batch := range slices.Chunk(jobs, 256) {
		values, err := collect(ctx, state.Query{Jobs: batch}, get, id)
		if err != nil {
			return nil, err
		}
		for _, value := range values {
			key := id(value)
			if !seen[key] {
				seen[key] = true
				result = append(result, value)
			}
		}
	}
	slices.SortFunc(result, func(a, b T) int { return strings.Compare(id(a), id(b)) })
	return result, nil
}
