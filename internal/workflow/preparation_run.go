package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/forge"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow/preparation"
)

var errNoSourceChanges = errors.New("Checksums are already current")

// errNothingToAmend is an update onto a contribution whose branch already
// holds it; the job completes and the contribution stays as it is.
var errNothingToAmend = errors.New("The branch already has this update; nothing to amend")

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
			finishJob(&job, record.JobCanceled, "Canceled before branch integration", e.now())
			changed = true
			return tx.PutJob(ctx, job)
		}
		if !job.Eligible(e.now()) {
			return nil
		}
		if job.Spec.Preparation == nil || e.Repo == nil || e.Preparer == nil && !capturedCorrection(job.Spec.Preparation.Correction) {
			detail = "workflow: preparation requires bound source, author, platform, Git, and a preparer"
			if err := closeEmptyContribution(ctx, tx, job.ChangeID); err != nil {
				return err
			}
			finishJob(&job, record.JobNeedsAttention, detail, e.now())
			changed = true
			return tx.PutJob(ctx, job)
		}
		if job.Spec.Action == record.Bump && job.ResolvedRelease == nil {
			budget = c.timeouts.Resolve
		}
		if err := c.take(&job.Lease, e.now(), budget); err != nil {
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
		if err := claimGuard(job, record.PhasePreparation, selected.Claim, e.now()); err != nil || job.Prepared != nil {
			return ErrClaimLost
		}
		job.Release()
		if job.CancelRequestedAt != nil {
			finishJob(&job, record.JobCanceled, "Canceled before branch integration", e.now())
		} else if errors.Is(operationErr, errNoSourceChanges) || errors.Is(operationErr, errNothingToAmend) {
			if err := closeEmptyContribution(ctx, tx, job.ChangeID); err != nil {
				return err
			}
			finishJob(&job, record.JobCompleted, operationErr.Error(), e.now())
		} else if operationErr != nil {
			detail = operationErr.Error()
			var limited *forge.RateLimitError
			if errors.As(operationErr, &limited) {
				c.fail(&job.Lease, string(job.ID), operationErr)
				job.Detail = detail
			} else {
				// A preparation that stops before any branch leaves nothing to
				// pursue; the contribution retires so the port's next bump starts
				// clean instead of competing with an empty open contribution.
				if err := closeEmptyContribution(ctx, tx, job.ChangeID); err != nil {
					return err
				}
				finishJob(&job, record.JobNeedsAttention, detail, e.now())
			}
		} else if resolving {
			job.ConsecutiveFailures = 0
			job.ResolvedRelease = &release
			if release.NoUpdate {
				if err := closeEmptyContribution(ctx, tx, job.ChangeID); err != nil {
					return err
				}
				finishJob(&job, record.JobCompleted, fmt.Sprintf("Already current at %s; latest eligible version is %s", release.CurrentVersion, release.Version), e.now())
			} else {
				label := release.Tag
				if release.Archive {
					label = "archive version " + release.Version
				}
				if release.LeavesStable {
					label += " (prerelease; this takes the port out of stable)"
				}
				job.Detail = "Resolved " + label + "; awaiting source preparation"
			}
		} else {
			job.ConsecutiveFailures = 0
			job.Prepared = &candidate
			job.Detail = "Prepared candidate; awaiting branch integration"
		}
		return tx.PutJob(ctx, job)
	})
	return changed, detail, err
}

func preparationRequest(job record.Job) preparation.Request {
	target := job.Spec.Targets[0]
	// The selection was resolved at binding: the target is the carrying
	// subport and the intent names the stub, which the editor honors.
	selection := macports.Selection{Selector: target.Portfile, Subport: target.Subport, Variants: target.Variants}
	return preparation.Request{EditIntent: job.Spec.Preparation.EditIntent, Action: job.Spec.Action, Source: job.Spec.Source,
		Selection: selection,
		Platform:  job.Spec.Preparation.Platform, Subject: job.Spec.Subject, References: job.Spec.References, Version: job.Spec.Version, Release: job.ResolvedRelease}
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
	correction := choices.Correction
	if capturedCorrection(correction) {
		// An amend or rebase captured its candidate at binding; the job
		// integrates it.
		return record.PreparedChange{Scope: correction.Scope, Branch: correction.Branch, Source: correction.Candidate}, nil
	}
	result, err := e.Preparer.Prepare(ctx, preparationRequest(job))
	if err != nil {
		return record.PreparedChange{}, err
	}
	if (job.Spec.Action == record.Bump && (result.Release == nil || *result.Release != *job.ResolvedRelease)) || result.Base != job.Spec.Source || record.CompareTargets(result.Target, target) != 0 || !git.ValidObjectID(string(result.PreparedTree)) {
		return record.PreparedChange{}, fmt.Errorf("workflow: preparation result does not match accepted input")
	}
	if job.Spec.Action == record.RefreshChecksums && result.PreparedTree == job.Spec.Source.Tree && len(result.Commits) == 0 && len(result.Files) == 0 {
		return record.PreparedChange{}, errNoSourceChanges
	}
	if correction != nil && result.PreparedTree == job.Spec.Source.Tree {
		return record.PreparedChange{}, errNothingToAmend
	}
	if !result.Scope.Valid() || result.Scope != nil && !choices.SharedRelease {
		return record.PreparedChange{}, fmt.Errorf("workflow: unapproved shared-release scope")
	}
	if len(result.Commits) != 1 {
		return record.PreparedChange{}, fmt.Errorf("workflow: preparation must produce one commit")
	}
	intent := result.Commits[0]
	signature := git.Signature{Name: choices.Author.Name, Email: choices.Author.Email, When: job.AcceptedAt}
	message := intent.Message()
	// A fresh preparation commits on the source it edited, master, with a
	// message of its own; an update onto a contribution commits on the
	// contribution's base, so the contribution stays one commit, lands on
	// its own branch, and keeps the message its author wrote.
	parent := job.Spec.Source.Commit
	if correction != nil {
		parent = job.Spec.Source.Base
		original, err := e.Repo.CommitMessage(ctx, string(job.Spec.Source.Commit))
		if err != nil {
			return record.PreparedChange{}, err
		}
		if message, err = revisedMessage(original, target.Name, job.Spec.Subject, job.Spec.References); err != nil {
			return record.PreparedChange{}, err
		}
	}
	commit, err := e.Repo.WriteCommit(ctx, git.Commit{Tree: string(result.PreparedTree), Parents: []string{string(parent)}, Message: message, Author: signature, Committer: signature})
	if err != nil {
		return record.PreparedChange{}, err
	}
	if correction != nil {
		candidate := record.Source{Commit: record.ObjectID(commit), Tree: result.PreparedTree, Base: job.Spec.Source.Base}
		scope, err := e.revisedScope(ctx, correction.Scope, result.Scope, candidate, choices.Platform)
		if err != nil {
			return record.PreparedChange{}, err
		}
		return record.PreparedChange{Scope: scope, Branch: correction.Branch, Source: candidate, PatchProblems: result.PatchProblems()}, nil
	}
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, strings.ToLower(initiatingName(job.Spec)))
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
	if job.Spec.Action == record.RefreshChecksums {
		prefix = "dockhand/checksums/"
	}
	branch := prefix + name + "-" + strings.ToLower(strings.TrimPrefix(string(job.ID), "job_"))
	return record.PreparedChange{Scope: result.Scope, Branch: branch, Source: record.Source{Commit: record.ObjectID(commit), Tree: result.PreparedTree, Base: job.Spec.Source.Base}, PatchProblems: result.PatchProblems()}, nil
}

// capturedCorrection reports whether a correction carries the candidate an
// amend or rebase captured at binding, which the job integrates rather than
// prepares.
func capturedCorrection(correction *record.CorrectionSpec) bool {
	return correction != nil && correction.Candidate.Tree != ""
}

// revisedMessage is a contribution's commit message after an update onto
// it: the subject replaced by the update's, under the name the message
// already carries, which keeps a stub's name over its carrying subport's,
// and the update's references added where the message does not cite them.
// Nothing else the author wrote changes. Corrections and updates prepared
// onto a contribution compose their messages this way.
func revisedMessage(original, targetName, subject string, references []record.Reference) (string, error) {
	var line string
	if subject != "" {
		name := targetName
		first, _, _ := strings.Cut(original, "\n")
		if prefix, _, ok := strings.Cut(first, ": "); ok && macports.ValidName(prefix) {
			name = prefix
		}
		var err error
		if line, err = portedit.Subject(name, subject); err != nil {
			return "", err
		}
	}
	return portedit.Rewrite(original, line, references), nil
}

// revisedScope is the release scope of a revision prepared onto a
// contribution, settled before the branch moves: the edit's own when it
// produced one, which must keep the contribution's membership, and the
// contribution's otherwise, rebound onto the candidate as a correction
// rebinds it. A contribution's membership is fixed when it is created; an
// update that would change it is a new bump, not an amendment.
func (e *Engine) revisedScope(ctx context.Context, prior, edited *record.ReleaseScope, candidate record.Source, platform record.Platform) (*record.ReleaseScope, error) {
	if edited != nil {
		if prior != nil && !prior.SameMembership(edited) {
			return nil, fmt.Errorf("%w: the update would change the contribution's release scope; start a new bump", ErrInvalidRequest)
		}
		return edited, nil
	}
	return e.rebindReleaseScope(ctx, prior, candidate, platform)
}
