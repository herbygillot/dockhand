package workflow

import (
	"context"
	"fmt"
	"github.com/herbygillot/dockhand/internal/macports/commitmsg"
	"maps"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

// contribution is an open contribution as a revision of it is bound: the
// change, its current revision, its attached pull request when it has one,
// and the remote head a replacement expects. It is the one owner of what a
// revision must keep, whether an update is prepared onto the contribution
// or a person corrects it: the revision's release scope, settled before the
// branch moves; the pull request's destination, whoever owns its head; the
// author's commit message; and the preconditions integration rechecks.
type contribution struct {
	change   record.Change
	revision record.Revision
	attached *record.PullRequest
	// remoteHead is the pull request's head as last observed when the
	// pull request is on the contribution's branch, the revision's commit
	// when there is no pull request, and empty when the pull request's
	// head is elsewhere.
	remoteHead record.ObjectID
}

// bindContribution reads a contribution and freezes the preconditions its
// integration checks: it is open on a branch, its revision is current, no
// other job is pending on it, and an attached pull request is open.
func (e *Engine) bindContribution(ctx context.Context, id record.ChangeID) (contribution, error) {
	var result contribution
	err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		change, err := r.Change(ctx, id)
		if err != nil {
			return err
		}
		if change.Disposition != record.ChangeOpen || change.Branch == "" || change.CurrentRevision == "" {
			return fmt.Errorf("%w: contribution %s is not open on a branch", ErrInvalidRequest, id)
		}
		if err := correctionIdle(ctx, r, change.ID, ""); err != nil {
			return err
		}
		revision, err := r.Revision(ctx, change.CurrentRevision)
		if err != nil {
			return err
		}
		result = contribution{change: change, revision: revision, remoteHead: revision.Source.Commit}
		if change.PullRequestID != "" {
			pr, err := r.PullRequest(ctx, change.PullRequestID)
			if err != nil {
				return err
			}
			if pr.State != record.PullRequestOpen {
				return fmt.Errorf("workflow: associated PR is %s", pr.State)
			}
			result.attached = &pr
			if pr.HeadBranch == change.Branch {
				result.remoteHead = pr.RemoteHead
			} else {
				result.remoteHead = ""
			}
		}
		return nil
	})
	return result, err
}

// correction is the replacement of the contribution's branch head that a
// revision of it records: the revision's scope, the head the branch must
// still have, and the candidate when one was captured at binding.
func (c contribution) correction(previousHead record.ObjectID, candidate record.Source) record.CorrectionSpec {
	return record.CorrectionSpec{Scope: c.revision.Scope, ChangeID: c.change.ID, RevisionID: c.revision.ID, Branch: c.change.Branch, PreviousHead: previousHead, RemoteHead: c.remoteHead, Candidate: candidate}
}

// title is the attached pull request's, or nothing.
func (c contribution) title() string {
	if c.attached == nil {
		return ""
	}
	return c.attached.Title
}

// allowsPublication refuses the one publication a contribution adopted
// with --keep-body cannot take: a rewrite of its author's body.
func (c contribution) allowsPublication(options *publish.Options) error {
	if c.change.KeepBody && options != nil && options.RefreshBody {
		return fmt.Errorf("%w: %s was adopted with --keep-body; its pull request body is its author's", ErrInvalidRequest, initiatingNameOf(c.change))
	}
	return nil
}

// destination is where a publication of the contribution goes: its pull
// request's base and head when it has one, the checkout's remotes otherwise.
func (e *Engine) destination(ctx context.Context, c *contribution, options publish.Options) (record.PublicationDestination, error) {
	var attached *record.PullRequest
	if c != nil {
		attached = c.attached
	}
	return e.publicationDestinationFor(ctx, attached, options)
}

// selection is the contribution's one target with the person's variant
// choices laid over its own; an update is prepared onto one target.
func (c contribution) selection(chosen map[string]bool) (macports.Selection, error) {
	if len(c.change.Targets) != 1 {
		return macports.Selection{}, fmt.Errorf("%w: contribution %s has %d targets; prepare onto one-target contributions only", ErrInvalidRequest, c.change.ID, len(c.change.Targets))
	}
	target := c.change.Targets[0]
	variants := maps.Clone(target.Variants)
	if variants == nil {
		variants = map[string]bool{}
	}
	maps.Copy(variants, chosen)
	return macports.Selection{Selector: target.Portfile, Subport: target.Subport, Variants: variants}, nil
}

// ContributionSubject is the subject for an update prepared onto a
// contribution: the one given, or the contribution's own, read from its
// commit after the port name, so the commit keeps the message it had. A
// revision bump still wants a subject, and the contribution's is the
// truthful one.
func ContributionSubject(ctx context.Context, repo *git.Repository, source record.Source, subject string) (string, error) {
	if subject != "" {
		return subject, nil
	}
	message, err := repo.CommitMessage(ctx, string(source.Commit))
	if err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(message, "\n")
	if _, after, ok := strings.Cut(first, ": "); ok {
		return after, nil
	}
	return first, nil
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
		if line, err = commitmsg.Subject(name, subject); err != nil {
			return "", err
		}
	}
	return commitmsg.Rewrite(original, line, references), nil
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
