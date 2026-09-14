package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

func finishPreparation(job *record.Job, state record.JobState, detail string, now time.Time) {
	job.State, job.Detail, job.FinishedAt = state, detail, &now
	job.Claim, job.RetryAt = nil, nil
}

func (c *cycle) integratePreparation(ctx context.Context, candidate record.Job) (changed bool, detail string, err error) {
	e := c.engine
	if e.Repo == nil {
		return false, "", fmt.Errorf("workflow: Git repository required to reconcile preparation")
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeouts.Prepare)
	defer cancel()
	err = e.Repo.WithBranchLock(callCtx, candidate.Prepared.Branch, func(ctx context.Context) error {
		var selected record.Job
		var recovering bool
		err := e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
			job, err := tx.Job(ctx, candidate.ID)
			if err != nil {
				return err
			}
			if job.State.Terminal() || job.Phase != record.PhasePreparation || job.ResultRevision != "" || job.Claim.Live(e.now()) || !due(job.RetryAt, e.now()) {
				return nil
			}
			if job.Prepared == nil || job.Prepared.Branch != candidate.Prepared.Branch || job.Spec.Preparation == nil {
				return state.ErrInvalid
			}
			if job.CancelRequestedAt != nil && !job.Prepared.IntegrationStarted {
				finishPreparation(&job, record.JobCanceled, "Canceled before branch integration", e.now())
				changed = true
				return tx.PutJob(ctx, job)
			}
			recovering = job.Prepared.IntegrationStarted
			prepared := *job.Prepared
			prepared.IntegrationStarted = true
			job.Prepared = &prepared
			job.Claim, err = c.claim(&job.ClaimGeneration, e.now(), c.timeouts.Prepare)
			if err != nil {
				return err
			}
			job.RetryAt = nil
			job.Detail = "Integrating prepared branch"
			selected, changed = job, true
			return tx.PutJob(ctx, job)
		})
		if err != nil || selected.ID == "" {
			return err
		}
		confirmed, operationErr := c.integrateBranch(ctx, selected, recovering)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
			job, err := tx.Job(ctx, selected.ID)
			if err != nil {
				return err
			}
			if job.State.Terminal() || job.Phase != record.PhasePreparation || job.ResultRevision != "" || !job.Claim.Owns(selected.Claim, e.now()) {
				return ErrClaimLost
			}
			job.Claim, job.RetryAt = nil, nil
			if operationErr != nil {
				detail = operationErr.Error()
				if errors.Is(operationErr, git.ErrRefUpdateUncertain) {
					retry := e.now().Add(c.retry)
					job.RetryAt, job.Detail = &retry, detail
				} else {
					finishPreparation(&job, record.JobNeedsAttention, detail, e.now())
				}
				return tx.PutJob(ctx, job)
			}
			if confirmed {
				change := record.Change{ID: record.ChangeID("change_" + string(job.ID)), Branch: job.Prepared.Branch, Targets: job.Spec.Targets, Disposition: record.ChangeOpen, CreatedAt: job.AcceptedAt}
				revision := record.Revision{ID: record.RevisionID("revision_" + string(job.ID)), ChangeID: change.ID, Source: job.Prepared.Source, CreatedAt: job.AcceptedAt}
				if err := tx.PutChange(ctx, change); err != nil {
					return err
				}
				if err := tx.PutRevision(ctx, revision); err != nil {
					return err
				}
				change.CurrentRevision = revision.ID
				if err := tx.PutChange(ctx, change); err != nil {
					return err
				}
				job.ChangeID, job.ResultRevision = change.ID, revision.ID
			}
			if job.CancelRequestedAt != nil {
				finishPreparation(&job, record.JobCanceled, "Canceled; any integrated branch is preserved", e.now())
			} else if !confirmed {
				return fmt.Errorf("%w: branch integration has no conclusive outcome", state.ErrInvalid)
			} else if job.Spec.Destination == record.BranchReady {
				finishPreparation(&job, record.JobCompleted, "Prepared branch "+job.Prepared.Branch, e.now())
			} else {
				job.Phase = record.PhaseVerification
				job.State, job.Detail = record.JobActive, "Prepared branch "+job.Prepared.Branch+"; verification pending"
			}
			return tx.PutJob(ctx, job)
		})
	})
	if ctx.Err() == nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
		detail, err = "Branch integration remains pending: "+err.Error(), nil
	}
	return changed, detail, err
}

func (c *cycle) integrateBranch(ctx context.Context, job record.Job, recovering bool) (bool, error) {
	e := c.engine
	registered, err := e.State.FindRepository(ctx, e.Repo.CommonDir)
	if err != nil {
		return false, err
	}
	if registered.ID != e.Repository {
		return false, fmt.Errorf("workflow: integration repository mismatch")
	}
	prepared := job.Prepared
	trees, err := e.Repo.CommitTrees(ctx, []string{string(prepared.Source.Commit)})
	if err != nil {
		return false, err
	}
	if trees[string(prepared.Source.Commit)] != string(prepared.Source.Tree) {
		return false, fmt.Errorf("workflow: prepared commit/tree mismatch")
	}
	refName := "refs/heads/" + prepared.Branch
	actual, err := e.Repo.ReadRef(ctx, refName)
	if err != nil {
		return false, err
	}
	wanted := git.RefValue{Exists: true, Object: string(prepared.Source.Commit)}
	if actual == wanted {
		return true, nil
	}
	if actual.Exists {
		return false, fmt.Errorf("%w: destination branch %s contains a different commit", git.ErrRefConflict, prepared.Branch)
	}
	if job.CancelRequestedAt != nil {
		return false, nil
	}
	if recovering {
		return false, fmt.Errorf("workflow: interrupted integration has no branch %s; refusing to recreate a possibly deleted branch", prepared.Branch)
	}
	err = e.Repo.UpdateRefs(ctx, []git.RefChange{
		{Name: refName, Desired: wanted},
	})
	if err != nil {
		return false, err
	}
	actual, err = e.Repo.ReadRef(ctx, refName)
	if err != nil {
		return false, err
	}
	if actual != wanted {
		return false, fmt.Errorf("%w: prepared branch changed during integration", git.ErrRefConflict)
	}
	return true, nil
}
