package workflow

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

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

// onto is what a preparation onto an open contribution binds to: the
// contribution, its current revision as the source to edit, and the
// correction that replaces its branch head when the candidate is ready.
type onto struct {
	change   record.Change
	revision record.Revision
	spec     record.CorrectionSpec
	// attached is the contribution's open pull request, when it has one:
	// the destination a publication of the update keeps.
	attached *record.PullRequest
}

// bindOnto reads the contribution an update is prepared onto and freezes
// the preconditions its integration checks: the revision is current, the
// branch is the contribution's, no other job is pending on it, and an
// attached pull request is open. It carries what the revision must keep:
// the contribution's release scope and its pull request.
func (e *Engine) bindOnto(ctx context.Context, id record.ChangeID) (onto, error) {
	var result onto
	err := e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		change, err := r.Change(ctx, id)
		if err != nil {
			return err
		}
		if change.Disposition != record.ChangeOpen || change.Branch == "" || change.CurrentRevision == "" {
			return fmt.Errorf("%w: contribution %s is not open on a branch", ErrInvalidRequest, id)
		}
		if len(change.Targets) != 1 {
			return fmt.Errorf("%w: contribution %s has %d targets; prepare onto one-target contributions only", ErrInvalidRequest, id, len(change.Targets))
		}
		if err := correctionIdle(ctx, r, change.ID, ""); err != nil {
			return err
		}
		revision, err := r.Revision(ctx, change.CurrentRevision)
		if err != nil {
			return err
		}
		remoteHead := revision.Source.Commit
		var attached *record.PullRequest
		if change.PullRequestID != "" {
			pr, err := r.PullRequest(ctx, change.PullRequestID)
			if err != nil {
				return err
			}
			if pr.State != record.PullRequestOpen {
				return fmt.Errorf("workflow: associated PR is %s", pr.State)
			}
			if pr.HeadBranch == change.Branch {
				remoteHead = pr.RemoteHead
			} else {
				remoteHead = ""
			}
			attached = &pr
		}
		result = onto{change: change, revision: revision, attached: attached, spec: record.CorrectionSpec{Scope: revision.Scope, ChangeID: change.ID, RevisionID: revision.ID, Branch: change.Branch, PreviousHead: revision.Source.Commit, RemoteHead: remoteHead}}
		return nil
	})
	return result, err
}

// selection is the contribution's target with the person's variant choices
// laid over its own.
func (o onto) selection(chosen map[string]bool) macports.Selection {
	target := o.change.Targets[0]
	variants := maps.Clone(target.Variants)
	if variants == nil {
		variants = map[string]bool{}
	}
	maps.Copy(variants, chosen)
	return macports.Selection{Selector: target.Portfile, Subport: target.Subport, Variants: variants}
}
