package workflow

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/changeset"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

var errPublicationCanceled = errors.New("publication canceled; any pushed branch is preserved")

func (c *cycle) advancePublication(ctx context.Context, id record.JobID) (bool, string, error) {
	e := c.engine
	var job record.Job
	var action record.PublicationAction
	claimed := false
	rejected := ""
	err := e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		var err error
		job, err = tx.Job(ctx, id)
		if err != nil {
			return err
		}
		if job.State.Terminal() || job.Phase != record.PhasePublication || !job.Eligible(e.now()) {
			return nil
		}
		action, err = tx.PublicationForJob(ctx, id)
		if errors.Is(err, state.ErrNotFound) && job.Spec.PublishTo != nil {
			err = nil
		}
		if err != nil {
			return err
		}
		if action.ID != "" {
			if policyErr := validatePublicationAction(job, action); policyErr != nil {
				rejected = policyErr.Error()
				action.State, action.LastError = record.PublicationNeedsAttention, rejected
				finishJob(&job, record.JobNeedsAttention, rejected, e.now())
				if err := tx.PutPublication(ctx, action); err != nil {
					return err
				}
				return tx.PutJob(ctx, job)
			}
		}
		if err := c.take(&job.Lease, e.now(), c.timeouts.Publish); err != nil {
			return err
		}
		job.State, job.RetryAt = record.JobActive, nil
		claimed = true
		return tx.PutJob(ctx, job)
	})
	if err != nil {
		return false, "", err
	}
	if rejected != "" {
		return true, rejected, nil
	}
	if !claimed {
		return false, "", nil
	}
	if action.ID == "" {
		return c.planPublication(ctx, job)
	}
	if e.Publisher == nil || e.Publisher.Repo == nil || e.Publisher.Forge == nil {
		// Another driver may be configured with a publisher, so this backs off rather than settling.
		err = c.publicationRetry(ctx, job, failuref("publication service is unavailable"))
		return true, "publication service is unavailable", err
	}
	call, cancel := context.WithTimeout(ctx, c.timeouts.Publish)
	defer cancel()
	err = e.Publisher.Repo.WithRemoteBranchLock(call, action.Spec.LockDirectory, action.Spec.Forge, action.Spec.HeadRepository, action.Spec.HeadBranch, func(locked context.Context) error {
		if err := c.publicationUpdate(locked, job, func(_ state.Tx, current *record.Job, stored *record.PublicationAction) error {
			action = *stored
			return nil
		}); err != nil {
			return err
		}
		return c.runPublication(locked, job, action)
	})
	if err == nil {
		return true, "", nil
	}
	if errors.Is(err, ErrClaimLost) || errors.Is(err, state.ErrConflict) || ctx.Err() != nil {
		return true, "", err
	}
	// Only failures known to precede a PR request can release this head for a new job.
	var current record.PublicationAction
	readErr := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		var err error
		current, err = r.PublicationForJob(ctx, id)
		return err
	})
	if readErr != nil {
		return true, "", errors.Join(err, readErr)
	}
	if !current.WriteStarted && (errors.Is(err, publish.ErrPrecondition) || errors.Is(err, git.ErrRefConflict) || errors.Is(err, ErrStaleRevision) || errors.Is(err, errPublicationCanceled)) {
		result := record.JobNeedsAttention
		if errors.Is(err, ErrStaleRevision) {
			result = record.JobSuperseded
		}
		if errors.Is(err, errPublicationCanceled) {
			result = record.JobCanceled
		}
		return true, err.Error(), c.finishPublication(ctx, job, result, err.Error(), nil)
	}
	return true, err.Error(), c.publicationRetry(ctx, job, failure(err))
}

func (c *cycle) publicationUpdate(ctx context.Context, expected record.Job, fn func(state.Tx, *record.Job, *record.PublicationAction) error) error {
	e := c.engine
	return e.State.Update(ctx, e.Repository, func(ctx context.Context, tx state.Tx) error {
		job, err := tx.Job(ctx, expected.ID)
		if err != nil {
			return err
		}
		if err := claimGuard(job, record.PhasePublication, expected.Claim, e.now()); err != nil {
			return err
		}
		action, err := tx.PublicationForJob(ctx, job.ID)
		if err != nil {
			return err
		}
		if err := fn(tx, &job, &action); err != nil {
			return err
		}
		if err := tx.PutPublication(ctx, action); err != nil {
			return err
		}
		return tx.PutJob(ctx, job)
	})
}

func (c *cycle) authorizePublication(ctx context.Context, expected record.Job, write bool) error {
	return c.publicationUpdate(ctx, expected, func(tx state.Tx, job *record.Job, action *record.PublicationAction) error {
		if action.WriteStarted {
			return fmt.Errorf("%w: PR request already started", state.ErrConflict)
		}
		if job.CancelRequestedAt != nil {
			return errPublicationCanceled
		}
		change, err := tx.Change(ctx, job.ChangeID)
		if err != nil {
			return err
		}
		revisionID, _ := publicationInput(*job)
		if change.Disposition != record.ChangeOpen || change.CurrentRevision != revisionID || change.Branch != action.Spec.SourceBranch() {
			return ErrStaleRevision
		}
		if err := publicationEvidence(ctx, tx, *job, action.Spec); err != nil {
			return err
		}
		action.State, action.PushStarted = record.PublicationApplying, true
		if write {
			action.WriteStarted = true
		}
		return nil
	})
}

func (c *cycle) runPublication(ctx context.Context, job record.Job, action record.PublicationAction) error {
	s := c.engine.Publisher
	spec := action.Spec
	_, source := publicationInput(job)
	if !action.WriteStarted {
		if err := c.publicationUpdate(ctx, job, func(tx state.Tx, current *record.Job, _ *record.PublicationAction) error {
			if current.CancelRequestedAt != nil {
				return errPublicationCanceled
			}
			change, err := tx.Change(ctx, current.ChangeID)
			if err != nil {
				return err
			}
			revisionID, _ := publicationInput(*current)
			if change.CurrentRevision != revisionID || change.Disposition != record.ChangeOpen {
				return ErrStaleRevision
			}
			return publicationEvidence(ctx, tx, *current, action.Spec)
		}); err != nil {
			return err
		}
		if _, err := s.SourceContent(ctx, source, job.Spec.Targets); err != nil {
			return err
		}
		// Validate accepted source again under the operation lock, outside the transaction.
		snapshot, err := changeset.CaptureBranch(ctx, s.Repo, spec.SourceBranch())
		if err != nil {
			return fmt.Errorf("%w: %v", publish.ErrPrecondition, err)
		}
		if snapshot.Commit != source.Commit || snapshot.Tree != source.Tree {
			return ErrStaleRevision
		}
	}
	observed, err := s.Observe(ctx, spec)
	if err != nil {
		return err
	}
	remote, err := s.Repo.RemoteHead(ctx, spec.PushURL, spec.HeadBranch)
	if err != nil {
		return err
	}
	desired := git.RefValue{Exists: true, Object: string(spec.Desired.Head)}
	if remote == desired && publish.Matches(spec, observed) {
		return c.finishPublication(ctx, job, record.JobCompleted, fmt.Sprintf("Published %s from verified branch %s:%s at %s", observed.PullRequest.Ref.URL, spec.HeadRepository, spec.HeadBranch, spec.Desired.Head), &observed.PullRequest)
	}
	if action.WriteStarted {
		return c.publicationRetry(ctx, job, waitingFor("PR request outcome is unresolved; observing without repeating the write"))
	}
	if err := s.Repo.CheckContributionBase(ctx, spec.BaseURL, spec.BaseBranch, string(source.Base), string(source.Commit)); err != nil {
		return err
	}
	if err := publish.CheckMetadata(spec, observed); err != nil {
		return err
	}
	if remote != desired {
		expected := git.RefValue{Exists: spec.ExpectedRemoteHead.Exists, Object: string(spec.ExpectedRemoteHead.Commit)}
		if remote != expected {
			return &git.RefConflict{Name: spec.HeadBranch, Expected: expected, Actual: remote}
		}
		if err := s.Preflight(ctx); err != nil {
			return err
		}
		if err := c.authorizePublication(ctx, job, false); err != nil {
			return err
		}
		if err := s.Repo.Push(ctx, git.Push{Remote: spec.PushURL, Branch: spec.HeadBranch, Commit: string(spec.Desired.Head), ExpectedRemote: expected}); err != nil {
			return err
		}
		return c.publicationRetry(ctx, job, waitingFor(fmt.Sprintf("Pushed verified branch %s:%s at %s; checking the remote before publishing", spec.HeadRepository, spec.HeadBranch, spec.Desired.Head)))
	}
	if observed.Found && observed.PullRequest.RemoteHead != spec.Desired.Head {
		return c.publicationRetry(ctx, job, waitingFor("Waiting for the forge to observe the pushed branch"))
	}
	if err := s.Preflight(ctx); err != nil {
		return err
	}
	if err := c.authorizePublication(ctx, job, true); err != nil {
		return err
	}
	_, err = s.Write(ctx, action)
	var limited *forge.RateLimitError
	if errors.As(err, &limited) {
		return c.publicationUpdate(ctx, job, func(_ state.Tx, current *record.Job, stored *record.PublicationAction) error {
			if !stored.WriteStarted || stored.WriteRefusals == ^uint32(0) {
				return state.ErrConflict
			}
			stored.WriteStarted = false
			stored.WriteRefusals++
			stored.State, stored.LastError = record.PublicationPending, err.Error()
			current.Release()
			c.fail(&current.Lease, string(current.ID), err)
			current.Detail = err.Error()
			return nil
		})
	}
	if errors.Is(err, forge.ErrRejected) {
		return c.finishPublication(ctx, job, record.JobNeedsAttention, err.Error(), nil)
	}
	if err != nil {
		return err
	}
	return c.publicationRetry(ctx, job, waitingFor(fmt.Sprintf("PR request sent for verified branch %s:%s at %s; awaiting confirmation", spec.HeadRepository, spec.HeadBranch, spec.Desired.Head)))
}

// publicationRetry releases the job for a later pass: a failure backs off and
// is retained as the action's last error; expected waiting uses the wait interval.
func (c *cycle) publicationRetry(ctx context.Context, expected record.Job, result outcome) error {
	return c.publicationUpdate(ctx, expected, func(_ state.Tx, job *record.Job, action *record.PublicationAction) error {
		if action.WriteStarted {
			action.State = record.PublicationUncertain
		}
		action.LastError = ""
		job.Release()
		if result.kind == failed {
			action.LastError = result.detail
			c.fail(&job.Lease, string(job.ID), result.err)
		} else {
			c.await(&job.Lease)
		}
		job.Detail = result.detail
		return nil
	})
}

func (c *cycle) finishPublication(ctx context.Context, expected record.Job, outcome record.JobState, detail string, observed *record.PullRequest) error {
	return c.publicationUpdate(ctx, expected, func(tx state.Tx, job *record.Job, action *record.PublicationAction) error {
		now := c.engine.now()
		action.State = record.PublicationNeedsAttention
		action.LastError = detail
		if observed != nil {
			change, err := tx.Change(ctx, job.ChangeID)
			if err != nil {
				return err
			}
			pr := *observed
			pr.ID, pr.ChangeID = change.PullRequestID, change.ID
			if pr.ID == "" {
				pr.ID = record.PullRequestID("pr_" + rand.Text())
			}
			if err := tx.PutPullRequest(ctx, pr); err != nil {
				return err
			}
			change.PullRequestID, change.PublishedRevision = pr.ID, action.RevisionID
			if err := tx.PutChange(ctx, change); err != nil {
				return err
			}
			action.State, action.ConfirmedAt, action.LastError = record.PublicationConfirmed, &now, ""
		}
		finishJob(job, outcome, detail, now)
		return nil
	})
}
