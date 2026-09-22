package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/git/changeset"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portedit"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

type CorrectionRequest struct {
	KeepFailed bool
	// Subject replaces the commit subject after the port name; References
	// are added to the trailers the commit does not already carry.
	Subject    string
	References []record.Reference
	ID         record.RequestID
	Action     record.Action
	// Target selects the contribution by port; Branch by its tracked branch;
	// neither means the current branch.
	Target string
	Branch string
	// Tree, when set, is a prepared tree that becomes the replacement
	// commit's contents in place of a checkout capture: an update dockhand
	// prepared onto the contribution's own revision.
	Tree record.ObjectID
	// Squash replaces the branch's commits, however many, with one commit of
	// the branch's tree on the contribution's base; the subject is the pull
	// request's title when one is attached. It is how a stacked pull request
	// becomes the one commit the port wants.
	Squash bool
	// Message, when set, is the whole commit message, as the person edited
	// it, in place of the one composed from the contribution's.
	Message           string
	Base              record.ObjectID
	Platform          record.Platform
	ResolveBuild      BuildResolver
	Publication       *publish.Options
	Preview           bool
	IncludeDependents bool
	// SkipVerify prepares the replacement without a build; with Publication it
	// publishes unverified, otherwise it stops at the branch.
	SkipVerify bool
}
type BoundCorrection struct {
	Request Request
	Diff    string
	Branch  string
	// Message is the replacement commit's message, for a person to edit.
	Message string
}

func correctionIdle(ctx context.Context, r state.Reader, id record.ChangeID, except record.JobID) error {
	jobs, err := r.Jobs(ctx, state.Query{ChangeID: id, Pending: true})
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.ID != except {
			return fmt.Errorf("workflow: change has pending job %s; wait or cancel before correcting its branch", job.ID)
		}
	}
	return nil
}
func correctionCurrent(ctx context.Context, r state.Reader, c record.CorrectionSpec) (record.Change, error) {
	change, err := r.Change(ctx, c.ChangeID)
	if err != nil {
		return change, err
	}
	if change.Disposition != record.ChangeOpen || change.CurrentRevision != c.RevisionID || change.Branch != c.Branch {
		return change, ErrStaleRevision
	}
	return change, nil
}

func (e *Engine) BindCorrection(ctx context.Context, input CorrectionRequest) (BoundCorrection, error) {
	var result BoundCorrection
	if e == nil || e.State == nil || e.Repo == nil || e.Ports == nil {
		return result, errNoState
	}
	if input.Subject != "" && (strings.TrimSpace(input.Subject) == "" || strings.ContainsAny(input.Subject, "\r\n\x00")) {
		return result, fmt.Errorf("workflow: subject must be one nonempty line")
	}
	for _, reference := range input.References {
		if !reference.Valid() {
			return result, fmt.Errorf("workflow: invalid reference %q", reference.Trailer())
		}
	}
	if input.Action != record.Amend && input.Action != record.Rebase {
		return result, ErrInvalidRequest
	}
	err := e.requireRepository(ctx, "")
	if err != nil {
		return result, err
	}
	// The contribution is resolved as a verification's is: by target, by
	// branch, or by the current branch; the transaction below rereads it
	// by that branch with the pull request it is attached to.
	selection := ResolutionRequest{Action: input.Action, Selection: macports.Selection{Selector: input.Target}, Branch: input.Branch, Platform: input.Platform}
	if input.Target == "" && input.Branch == "" {
		selection.Branch, err = e.Repo.CurrentBranch(ctx)
		if err != nil {
			return result, err
		}
	}
	resolution, err := e.Resolve(ctx, selection)
	if err != nil {
		return result, err
	}
	branch := resolution.Branch
	var change record.Change
	var revision record.Revision
	var remoteHead record.ObjectID
	var title string
	var attached *record.PullRequest
	err = e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		var err error
		change, err = r.OpenChangeByBranch(ctx, branch)
		if err != nil {
			return err
		}
		if err = correctionIdle(ctx, r, change.ID, ""); err != nil {
			return err
		}
		revision, err = r.Revision(ctx, change.CurrentRevision)
		if err != nil {
			return err
		}
		remoteHead = revision.Source.Commit
		if change.PullRequestID != "" {
			pr, err := r.PullRequest(ctx, change.PullRequestID)
			if err != nil {
				return err
			}
			if pr.State != record.PullRequestOpen {
				return fmt.Errorf("workflow: associated PR is %s", pr.State)
			}
			title = pr.Title
			attached = &pr
			if pr.HeadBranch == branch {
				remoteHead = pr.RemoteHead
			} else {
				remoteHead = ""
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	committed, err := changeset.CaptureBranch(ctx, e.Repo, branch)
	if err != nil {
		return result, err
	}
	snapshot := committed
	if change.KeepBody && input.Publication != nil && input.Publication.RefreshBody {
		return result, fmt.Errorf("%w: %s was adopted with --keep-body; its pull request body is its author's", ErrInvalidRequest, initiatingNameOf(change))
	}
	if input.Tree != "" {
		if !git.ValidObjectID(string(input.Tree)) {
			return result, fmt.Errorf("%w: prepared tree must be a literal object", ErrInvalidRequest)
		}
		snapshot.Tree, snapshot.ModifiedPaths, snapshot.UntrackedPaths = input.Tree, nil, nil
	} else if input.Squash {
		// The branch's tree as committed, folded onto the base below.
	} else if input.Action == record.Amend && input.Branch == "" {
		snapshot, err = changeset.CaptureCheckout(ctx, e.Repo)
		if err != nil {
			return result, err
		}
		if snapshot.Branch != branch || snapshot.Head != committed.Head {
			return result, ErrStaleRevision
		}
	}
	target, err := e.inferVerificationTarget(ctx, snapshot.Source(revision.Source.Base), change, macports.Selection{})
	if err != nil {
		return result, err
	}
	if err := checkUntracked(snapshot.UntrackedPaths, target.Portfile); err != nil {
		return result, err
	}
	base := revision.Source.Base
	if input.Action == record.Rebase {
		if input.Base == "" {
			return result, fmt.Errorf("workflow: rebase requires a frozen upstream base")
		}
		base = input.Base
	}
	author, err := e.Repo.Author(ctx)
	if err != nil {
		return result, err
	}
	author.When = e.now()
	messageCommit := revision.Source.Commit
	if messageCommit == "" {
		messageCommit = change.GeneratedCommit
	}
	if messageCommit == "" {
		messageCommit = committed.Commit
	}
	message, err := e.Repo.CommitMessage(ctx, string(messageCommit))
	if err != nil {
		return result, err
	}
	if input.Squash && title != "" {
		// A stacked pull request's commits say what they said; the title is
		// what its author meant, in the port's own words when they wrote it
		// that way, and with the port name supplied when they did not.
		if _, _, ok := strings.Cut(title, ": "); ok {
			message = title
		} else if message, err = portedit.Subject(target.Name, title); err != nil {
			return result, err
		}
	}
	if input.Subject != "" || len(input.References) > 0 {
		if message, err = revisedMessage(message, target.Name, input.Subject, input.References); err != nil {
			return result, err
		}
	}
	if input.Message != "" {
		first, _, _ := strings.Cut(input.Message, "\n")
		if strings.TrimSpace(first) == "" {
			return result, fmt.Errorf("%w: the edited message needs a subject line", ErrInvalidRequest)
		}
		message = strings.TrimRight(input.Message, "\n")
	}
	result.Message = message
	candidate, err := changeset.Correct(ctx, e.Repo, snapshot, revision.Source.Base, base, message, author)
	if err != nil {
		return result, err
	}
	targets, evaluation, err := e.bindSnapshot(ctx, candidate, macports.Selection{Selector: target.Portfile, Subport: target.Subport, Variants: target.Variants}, input.Platform, nil)
	if err != nil {
		return result, err
	}
	if _, err := e.inferVerificationTarget(ctx, candidate, change, macports.Selection{}); err != nil {
		return result, err
	}
	scope, err := e.rebindReleaseScope(ctx, revision.Scope, candidate, input.Platform)
	if err != nil {
		return result, err
	}
	diff, err := e.Repo.DiffTrees(ctx, string(revision.Source.Tree), string(candidate.Tree))
	if err != nil {
		return result, err
	}
	result.Branch, result.Diff = branch, string(diff)
	if input.Preview {
		return result, nil
	}
	var build BuildResolution
	if !input.SkipVerify {
		if input.ResolveBuild == nil {
			return result, ErrInvalidRequest
		}
		build, err = input.ResolveBuild(ctx, evaluation)
		if err != nil {
			return result, err
		}
	}
	spec := record.JobSpec{KeepFailed: input.KeepFailed, Action: input.Action, Source: committed.Source(revision.Source.Base), Targets: targets, Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: build.Build, BuildRequirements: build.Requirements, IncludeDependents: input.IncludeDependents, TargetBuilds: build.TargetBuilds,
		Preparation: &record.PreparationSpec{SourceBranch: branch, Platform: input.Platform, Author: record.CommitIdentity{Name: author.Name, Email: author.Email}, VerificationProblem: build.Problem,
			Correction: &record.CorrectionSpec{Scope: scope, ChangeID: change.ID, RevisionID: revision.ID, Branch: branch, PreviousHead: committed.Head, RemoteHead: remoteHead, Candidate: candidate}}}
	if input.SkipVerify {
		spec.Destination, spec.Verification = record.BranchReady, record.VerificationSkipped
	}
	if input.Publication != nil {
		destination, err := e.publicationDestinationFor(ctx, attached, *input.Publication)
		if err != nil {
			return result, err
		}
		spec.PublishTo, spec.Destination = &destination, record.Published
	}
	spec, err = normalizeSpec(spec)
	if err != nil {
		return result, err
	}
	result.Request = Request{ID: input.ID, Spec: spec}
	return result, nil
}

func correctionNotPending(ctx context.Context, r state.Reader, id record.ChangeID) error {
	jobs, err := r.Jobs(ctx, state.Query{ChangeID: id, Pending: true})
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.Spec.Preparation != nil && job.Spec.Preparation.Correction != nil {
			return fmt.Errorf("workflow: correction %s is pending; wait or cancel before adopting another revision", job.ID)
		}
	}
	return nil
}
