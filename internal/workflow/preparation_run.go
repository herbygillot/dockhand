package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/prepare"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

func (c *cycle) advancePreparation(ctx context.Context, id record.JobID) (bool, string, error) {
	e := c.engine
	var selected record.Job
	var changed bool
	var detail string
	budget := c.timeouts.Prepare
	err := e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		job, err := tx.Job(ctx, id)
		if err != nil {
			return err
		}
		if job.State.Terminal() || job.Phase != record.PhasePreparation {
			return nil
		}
		if job.Prepared != nil {
			selected = job
			return nil
		}
		if job.CancelRequestedAt != nil {
			finishPreparation(&job, record.JobCanceled, "Canceled before branch integration", e.now())
			changed = true
			return tx.PutJob(ctx, job)
		}
		if job.Claim.Live(e.now()) || !due(job.RetryAt, e.now()) {
			return nil
		}
		if job.Spec.Preparation == nil || e.Repo == nil || e.Preparer == nil {
			detail = "workflow: preparation requires bound source, author, platform, Git, and a preparer"
			finishPreparation(&job, record.JobNeedsAttention, detail, e.now())
			changed = true
			return tx.PutJob(ctx, job)
		}
		if job.Spec.Action == record.Bump && job.ResolvedRelease == nil {
			budget = c.timeouts.Resolve
		}
		job.Claim, err = c.claim(&job.ClaimGeneration, e.now(), budget)
		if err != nil {
			return err
		}
		job.State, job.Detail, job.RetryAt = record.JobActive, "Preparing "+string(job.Spec.Action), nil
		selected, changed = job, true
		return tx.PutJob(ctx, job)
	})
	if err != nil || selected.ID == "" {
		return changed, detail, err
	}
	if selected.Prepared != nil {
		return c.integratePreparation(ctx, selected)
	}
	callCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var candidate record.PreparedChange
	var release record.Release
	var operationErr error
	resolving := selected.Spec.Action == record.Bump && selected.ResolvedRelease == nil
	if resolving {
		if e.Releases == nil {
			operationErr = fmt.Errorf("workflow: release resolver is required")
		} else {
			release, operationErr = e.Releases.ResolveRelease(callCtx, preparationRequest(selected))
		}
	} else {
		candidate, operationErr = c.prepareCandidate(callCtx, selected)
	}
	if ctx.Err() != nil {
		return changed, "", ctx.Err()
	}
	operationErr = errors.Join(operationErr, callCtx.Err())
	err = e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		job, err := tx.Job(ctx, id)
		if err != nil {
			return err
		}
		if job.State.Terminal() || job.Prepared != nil || !job.Claim.Owns(selected.Claim, e.now()) {
			return ErrClaimLost
		}
		job.Claim, job.RetryAt = nil, nil
		if job.CancelRequestedAt != nil {
			finishPreparation(&job, record.JobCanceled, "Canceled before branch integration", e.now())
		} else if operationErr != nil {
			detail = operationErr.Error()
			finishPreparation(&job, record.JobNeedsAttention, detail, e.now())
		} else if resolving {
			job.ResolvedRelease = &release
			if release.NoUpdate {
				finishPreparation(&job, record.JobCompleted, fmt.Sprintf("Already current at %s; latest eligible version is %s", release.CurrentVersion, release.Version), e.now())
			} else {
				job.Detail = "Resolved " + release.Tag + "; awaiting source preparation"
			}
		} else {
			job.Prepared = &candidate
			job.Detail = "Prepared candidate; awaiting branch integration"
		}
		return tx.PutJob(ctx, job)
	})
	return changed, detail, err
}

func preparationRequest(job record.Job) prepare.Request {
	target := job.Spec.Targets[0]
	return prepare.Request{Action: job.Spec.Action, Source: job.Spec.Source,
		Selection: macports.Selection{Selector: target.Portfile, Subport: target.Subport, Variants: target.Variants},
		Platform:  job.Spec.Preparation.Platform, Reason: job.Spec.Reason, Version: job.Spec.Version, Release: job.ResolvedRelease}
}

func (c *cycle) prepareCandidate(ctx context.Context, job record.Job) (record.PreparedChange, error) {
	e := c.engine
	registered, err := e.State.FindRepository(ctx, e.Repo.CommonDir)
	if err != nil {
		return record.PreparedChange{}, err
	}
	if registered.ID != e.Repository {
		return record.PreparedChange{}, fmt.Errorf("workflow: preparation repository mismatch")
	}
	target := job.Spec.Targets[0]
	choices := job.Spec.Preparation
	result, err := e.Preparer.Prepare(ctx, preparationRequest(job))
	if err != nil {
		return record.PreparedChange{}, err
	}
	if (job.Spec.Action == record.Bump && (result.Release == nil || *result.Release != *job.ResolvedRelease)) || result.Base != job.Spec.Source || record.CompareTargets(result.Target, target) != 0 || !git.ValidObjectID(string(result.PreparedTree)) || len(result.Commits) != 1 {
		return record.PreparedChange{}, fmt.Errorf("workflow: preparation result does not match accepted input")
	}
	intent := result.Commits[0]
	signature := git.Signature{Name: choices.Author.Name, Email: choices.Author.Email, When: job.AcceptedAt}
	message := intent.Message()
	commit, err := e.Repo.WriteCommit(ctx, git.Commit{Tree: string(result.PreparedTree), Parents: []string{string(job.Spec.Source.Commit)}, Message: message, Author: signature, Committer: signature})
	if err != nil {
		return record.PreparedChange{}, err
	}
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, strings.ToLower(target.Name))
	if len(name) > 64 {
		name = name[:64]
	}
	name = strings.Trim(name, ".-")
	if name == "" {
		name = "port"
	}
	prefix := "dockhand/revbump/"
	if job.Spec.Action == record.Bump {
		prefix = "dockhand/bump/"
	}
	branch := prefix + name + "-" + strings.ToLower(strings.TrimPrefix(string(job.ID), "job_"))
	return record.PreparedChange{Branch: branch, Source: record.Source{Commit: record.ObjectID(commit), Tree: result.PreparedTree, Base: job.Spec.Source.Base}}, nil
}
