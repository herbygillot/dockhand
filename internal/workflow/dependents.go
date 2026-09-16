package workflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

type DependentDiscoverer interface {
	Discover(context.Context, record.Source, record.BuildConfig, []record.Target) (verify.Coverage, error)
}

// planDependents claims source-bound discovery, runs it outside the writer, and
// freezes its complete result. Subsequent cycles need only the recorded plan.
func (c *cycle) planDependents(ctx context.Context, id record.JobID) (bool, string, error) {
	e := c.engine
	var selected record.Job
	var revision record.Revision
	var ready, changed bool
	err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		_, err := r.Plan(ctx, id)
		if errors.Is(err, state.ErrNotFound) {
			return nil
		}
		ready = err == nil
		return err
	})
	if err != nil {
		return false, "", err
	}
	if ready {
		return c.advanceJob(ctx, id)
	}
	err = e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		job, err := tx.Job(ctx, id)
		if err != nil {
			return err
		}
		if job.State.Terminal() || job.Phase != record.PhaseVerification || job.Claim.Live(e.now()) || !due(job.RetryAt, e.now()) {
			return nil
		}
		if _, err := tx.Plan(ctx, id); err == nil {
			ready = true
			return nil
		} else if !errors.Is(err, state.ErrNotFound) {
			return err
		}
		if job.CancelRequestedAt != nil {
			finishPreparation(&job, record.JobCanceled, "Canceled before dependent discovery", e.now())
			changed = true
			return tx.PutJob(ctx, job)
		}
		if e.Dependents == nil || job.Spec.Build == nil {
			finishPreparation(&job, record.JobNeedsAttention, "Dependent discovery requires a source index and concrete local build configuration", e.now())
			changed = true
			return tx.PutJob(ctx, job)
		}
		revisionID, _ := publicationInput(job)
		if revisionID != "" {
			revision, err = tx.Revision(ctx, revisionID)
			if err != nil {
				return err
			}
		}
		job.Claim, err = c.claim(&job.ClaimGeneration, e.now(), c.timeouts.Prepare)
		if err != nil {
			return err
		}
		job.State, job.Detail, job.RetryAt = record.JobActive, "Discovering direct dependent coverage", nil
		selected, changed = job, true
		return tx.PutJob(ctx, job)
	})
	if err != nil || selected.ID == "" {
		if err == nil && ready {
			return c.advanceJob(ctx, id)
		}
		return changed, "", err
	}
	call, cancel := context.WithTimeout(ctx, c.timeouts.Prepare)
	defer cancel()
	_, source := publicationInput(selected)
	roots := selected.Spec.Targets
	if revision.Scope != nil {
		roots = revision.Scope.BuildTargets()
	}
	coverage, operationErr := e.Dependents.Discover(call, source, *selected.Spec.Build, roots)
	var plan record.VerificationPlan
	if operationErr == nil {
		plan, operationErr = verify.PlanDependents(selected, revision, coverage)
	}
	operationErr = errors.Join(operationErr, call.Err())
	if ctx.Err() != nil {
		return changed, "", ctx.Err()
	}
	var detail string
	err = e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		job, err := tx.Job(ctx, id)
		if err != nil {
			return err
		}
		if job.State.Terminal() || !job.Claim.Owns(selected.Claim, e.now()) {
			return ErrClaimLost
		}
		job.Claim, job.RetryAt = nil, nil
		switch {
		case job.CancelRequestedAt != nil:
			finishPreparation(&job, record.JobCanceled, "Canceled during dependent discovery", e.now())
		case operationErr != nil:
			detail = operationErr.Error()
			finishPreparation(&job, record.JobNeedsAttention, detail, e.now())
		default:
			if err := tx.PutPlan(ctx, plan); err != nil {
				return err
			}
			job.Detail = fmt.Sprintf("Planned %d isolated verification targets", len(plan.Targets))
		}
		return tx.PutJob(ctx, job)
	})
	return changed, detail, err
}
