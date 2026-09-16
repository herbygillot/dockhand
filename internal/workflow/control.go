package workflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// Control durably records an idempotent cancellation request for explicit jobs.
// Callers supply an ID, Cancel kind, job IDs, and optional reason; change/revision
// selectors and timestamps must be unset. Other control kinds remain unsupported.
//
// Repeated job IDs and their order do not change intent. An equivalent retry is
// a no-op; reuse of an ID for other intent returns ErrRequestConflict. A successful
// call records intent only. A later Cycle applies it and reconciles any remote
// cancellation. After an uncertain state commit, retry the original request.
func (e *Engine) Control(ctx context.Context, request record.ControlRequest) error {
	if e == nil || e.State == nil || e.Repository == "" {
		return ErrNoState
	}
	if request.Kind != record.Cancel {
		return fmt.Errorf("%w: control %s", ErrUnsupportedAction, request.Kind)
	}
	if !validToken(string(request.ID)) || len(request.Jobs) == 0 || request.ChangeID != "" || request.ExpectedRevision != "" || !request.SubmittedAt.IsZero() || request.AppliedAt != nil || !utf8.ValidString(request.Reason) {
		return fmt.Errorf("%w: cancellation requires a request ID and explicit job IDs; timestamps are driver-owned", ErrInvalidRequest)
	}
	request.Jobs = slices.Clone(request.Jobs)
	slices.Sort(request.Jobs)
	request.Jobs = slices.Compact(request.Jobs)
	for _, id := range request.Jobs {
		if !validToken(string(id)) {
			return fmt.Errorf("%w: invalid job ID", ErrInvalidRequest)
		}
	}
	return e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		accepted, err := tx.Request(ctx, request.ID)
		if err == nil {
			if accepted.Kind != record.CancelRequest {
				return ErrRequestConflict
			}
			previous, err := tx.Control(ctx, request.ID)
			if err != nil {
				return err
			}
			previous.SubmittedAt = request.SubmittedAt
			previous.AppliedAt = nil
			if !reflect.DeepEqual(previous, request) {
				return ErrRequestConflict
			}
			return nil
		}
		if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		for _, id := range request.Jobs {
			if _, err = tx.Job(ctx, id); err != nil {
				return err
			}
		}
		request.SubmittedAt = e.now()
		return tx.PutControl(ctx, request)
	})
}

// BranchScope freezes the queued and active jobs currently associated with one
// open tracked contribution. Jobs accepted after this read are not added.
func (e *Engine) BranchScope(ctx context.Context, branch string) (Scope, error) {
	return e.ContributionScope(ctx, ContributionSelector{Branch: branch})
}

// ContributionScope freezes pending jobs without incorporating later submissions.
func (e *Engine) ContributionScope(ctx context.Context, selected ContributionSelector) (Scope, error) {
	if e == nil || e.State == nil || e.Repository == "" {
		return Scope{}, ErrNoState
	}
	if err := selected.Validate(); err != nil {
		return Scope{}, err
	}
	var scope Scope
	err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		change, err := selectContribution(ctx, r, selected)
		if err != nil {
			return err
		}
		jobs, err := pendingJobIDs(ctx, r, change.ID)
		if err != nil {
			return err
		}
		if len(jobs) == 0 {
			return fmt.Errorf("%w for contribution %s", ErrNoPendingJobs, change.ID)
		}
		scope = Scope{Jobs: jobs}
		return nil
	})
	return scope, err
}

// ControlBranch selects a branch's queued and active jobs and records the exact
// cancellation set in the same transaction. Retrying an accepted request uses
// its original job set, even when newer work now exists on the branch.
func (e *Engine) ControlBranch(ctx context.Context, request record.ControlRequest, branch string) (Scope, error) {
	return e.ControlContribution(ctx, request, ContributionSelector{Branch: branch})
}

// ControlContribution selects and records cancellation in one transaction.
// A replay retains the original jobs even if later work has been accepted.
func (e *Engine) ControlContribution(ctx context.Context, request record.ControlRequest, selected ContributionSelector) (Scope, error) {
	if err := selected.Validate(); err != nil {
		return Scope{}, err
	}
	if e == nil || e.State == nil || e.Repository == "" {
		return Scope{}, ErrNoState
	}
	if request.Kind != record.Cancel {
		return Scope{}, fmt.Errorf("%w: control %s", ErrUnsupportedAction, request.Kind)
	}
	if !validToken(string(request.ID)) || len(request.Jobs) != 0 || request.ChangeID != "" || request.ExpectedRevision != "" || !request.SubmittedAt.IsZero() || request.AppliedAt != nil || !utf8.ValidString(request.Reason) {
		return Scope{}, fmt.Errorf("%w: branch cancellation requires a request ID and literal branch; selection and timestamps are driver-owned", ErrInvalidRequest)
	}
	var scope Scope
	err := e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		accepted, err := tx.Request(ctx, request.ID)
		if err == nil {
			if accepted.Kind != record.CancelRequest {
				return ErrRequestConflict
			}
			previous, err := tx.Control(ctx, request.ID)
			if err != nil {
				return err
			}
			if previous.Kind != request.Kind || previous.Reason != request.Reason {
				return ErrRequestConflict
			}
			if err = controlMatchesContribution(ctx, tx, previous, selected); err != nil {
				return err
			}
			scope = Scope{Jobs: slices.Clone(previous.Jobs)}
			return nil
		}
		if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		change, err := selectContribution(ctx, tx, selected)
		if err != nil {
			return err
		}
		jobs, err := pendingJobIDs(ctx, tx, change.ID)
		if err != nil {
			return err
		}
		if len(jobs) == 0 {
			return fmt.Errorf("%w for contribution %s", ErrNoPendingJobs, change.ID)
		}
		request.Jobs = jobs
		request.SubmittedAt = e.now()
		if err = tx.PutControl(ctx, request); err != nil {
			return err
		}
		scope = Scope{Jobs: slices.Clone(jobs)}
		return nil
	})
	return scope, err
}

func pendingJobIDs(ctx context.Context, r state.Reader, change record.ChangeID) ([]record.JobID, error) {
	result := []record.JobID{}
	query := state.Query{ChangeID: change, Pending: true, Limit: 256}
	for {
		jobs, err := r.Jobs(ctx, query)
		if err != nil {
			return nil, err
		}
		for _, job := range jobs {
			result = append(result, job.ID)
		}
		if len(jobs) < query.Limit {
			return result, nil
		}
		query.After = string(jobs[len(jobs)-1].ID)
	}
}

func controlMatchesContribution(ctx context.Context, r state.Reader, request record.ControlRequest, selector ContributionSelector) error {
	var selected record.ChangeID
	for _, id := range request.Jobs {
		job, err := r.Job(ctx, id)
		if err != nil {
			return err
		}
		if job.ChangeID == "" || selected != "" && job.ChangeID != selected {
			return ErrRequestConflict
		}
		selected = job.ChangeID
	}
	if selected == "" {
		return ErrRequestConflict
	}
	change, err := r.Change(ctx, selected)
	if err != nil {
		return err
	}
	if selector.Branch != "" && change.Branch != selector.Branch || selector.ChangeID != "" && change.ID != selector.ChangeID || selector.Target != "" && !strings.EqualFold(change.InitiatingTarget, selector.Target) {
		return ErrRequestConflict
	}
	return nil
}
