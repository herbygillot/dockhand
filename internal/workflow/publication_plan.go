package workflow

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/changeset"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// planPublication checkpoints the prepared revision's publication after its
// verification settles. The accepted destination never comes from current Git
// config. External reads run outside transactions; pushing starts in a later pass.
func (c *cycle) planPublication(ctx context.Context, job record.Job) (bool, string, error) {
	e := c.engine
	if job.Phase != record.PhasePublication {
		return false, "", ErrInvalidRequest
	}
	call, cancel := context.WithTimeout(ctx, c.timeouts.Publish)
	defer cancel()
	fail := func(problem error) (bool, string, error) {
		if err := call.Err(); err != nil {
			problem = err
		}
		outcome := record.JobActive
		switch {
		case errors.Is(problem, errPublicationCanceled):
			outcome = record.JobCanceled
		case errors.Is(problem, ErrStaleRevision):
			outcome = record.JobSuperseded
		case errors.Is(problem, publish.ErrPrecondition), errors.Is(problem, git.ErrRefConflict), errors.Is(problem, state.ErrConflict), errors.Is(problem, ErrInvalidRequest):
			outcome = record.JobNeedsAttention
		}
		err := e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
			current, err := tx.Job(ctx, job.ID)
			if err != nil {
				return err
			}
			if current.State.Terminal() || !current.Claim.Owns(job.Claim, e.now()) {
				return ErrClaimLost
			}
			if current.CancelRequestedAt != nil {
				outcome, problem = record.JobCanceled, errPublicationCanceled
			}
			current.State, current.Claim, current.Detail = outcome, nil, problem.Error()
			current.RetryAt = nil
			if outcome == record.JobActive {
				retry := e.now().Add(c.retry)
				current.RetryAt = &retry
			} else {
				now := e.now()
				current.FinishedAt = &now
			}
			return tx.PutJob(ctx, current)
		})
		return true, problem.Error(), err
	}
	if job.CancelRequestedAt != nil {
		return fail(errPublicationCanceled)
	}
	if e.Publisher == nil || e.Publisher.Repo == nil || e.Publisher.Forge == nil {
		return fail(fmt.Errorf("publication service is unavailable"))
	}
	revisionID, source := publicationInput(job)
	var change record.Change
	var evidence record.Attempt
	var associated *record.PullRequest
	err := e.State.View(call, e.Repository, func(ctx context.Context, r state.Reader) error {
		var err error
		change, err = r.Change(ctx, job.ChangeID)
		if err != nil {
			return err
		}
		if job.Prepared == nil || change.CurrentRevision != revisionID || change.Disposition != record.ChangeOpen || change.Branch != job.Prepared.Branch {
			return ErrStaleRevision
		}
		if job.ReusedAttempt != "" {
			evidence, err = r.Attempt(ctx, job.ReusedAttempt)
		} else {
			var attempts []record.Attempt
			attempts, err = r.AttemptsForJob(ctx, job.ID)
			if err == nil && len(attempts) != 1 {
				return ErrInvalidRequest
			}
			if err == nil {
				evidence = attempts[0]
			}
		}
		if err != nil {
			return err
		}
		if change.PullRequestID != "" {
			pr, err := r.PullRequest(ctx, change.PullRequestID)
			if err != nil {
				return err
			}
			associated = &pr
		}
		return publicationEvidence(ctx, r, job, record.PublicationSpec{EvidenceAttempt: evidence.ID})
	})
	if err != nil {
		return fail(err)
	}
	snapshot, err := changeset.CaptureBranch(call, e.Publisher.Repo, change.Branch)
	if err != nil {
		return fail(fmt.Errorf("%w: %v", publish.ErrPrecondition, err))
	}
	if snapshot.Commit != source.Commit || snapshot.Tree != source.Tree {
		return fail(ErrStaleRevision)
	}
	spec, err := e.Publisher.PlanTo(call, change, source, evidence, associated, *job.Spec.PublishTo)
	if err != nil {
		return fail(err)
	}
	// Observe the branch again after remote reads; never adopt human edits implicitly.
	snapshot, err = changeset.CaptureBranch(call, e.Publisher.Repo, change.Branch)
	if err != nil {
		return fail(fmt.Errorf("%w: %v", publish.ErrPrecondition, err))
	}
	if snapshot.Commit != source.Commit || snapshot.Tree != source.Tree {
		return fail(ErrStaleRevision)
	}
	if err = call.Err(); err != nil {
		return fail(err)
	}
	err = e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		current, err := tx.Job(ctx, job.ID)
		if err != nil {
			return err
		}
		if current.State.Terminal() || !current.Claim.Owns(job.Claim, e.now()) {
			return ErrClaimLost
		}
		if current.CancelRequestedAt != nil {
			return errPublicationCanceled
		}
		change, err := tx.Change(ctx, current.ChangeID)
		if err != nil {
			return err
		}
		if change.CurrentRevision != revisionID || change.Disposition != record.ChangeOpen || change.Branch != spec.HeadBranch {
			return ErrStaleRevision
		}
		if err := publicationEvidence(ctx, tx, current, spec); err != nil {
			return err
		}
		action := record.PublicationAction{ID: record.PublicationID("publication_" + rand.Text()), JobID: job.ID, ChangeID: job.ChangeID, RevisionID: revisionID, Spec: spec, State: record.PublicationPending}
		if err := validatePublicationAction(current, action); err != nil {
			return err
		}
		if err := tx.PutPublication(ctx, action); err != nil {
			return err
		}
		current.Claim, current.RetryAt, current.Detail = nil, nil, "Verification passed; publication planned"
		return tx.PutJob(ctx, current)
	})
	if errors.Is(err, errPublicationCanceled) || errors.Is(err, ErrStaleRevision) || errors.Is(err, publish.ErrPrecondition) || errors.Is(err, state.ErrConflict) {
		return fail(err)
	}
	return true, "", err
}
