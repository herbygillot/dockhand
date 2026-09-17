package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/git/changeset"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
)

type CorrectionRequest struct {
	KeepFailed        bool
	Title             string
	ID                record.RequestID
	Action            record.Action
	Branch            string
	Base              record.ObjectID
	Platform          record.Platform
	ResolveBuild      BuildResolver
	Publication       *publish.Options
	Preview           bool
	IncludeDependents bool
}
type BoundCorrection struct {
	Request Request
	Diff    string
	Branch  string
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
	if input.Title != "" && (strings.TrimSpace(input.Title) == "" || strings.ContainsAny(input.Title, "\r\n\x00")) {
		return result, fmt.Errorf("workflow: title must be one nonempty line")
	}
	if input.Action != record.Amend && input.Action != record.Rebase {
		return result, ErrInvalidRequest
	}
	registered, err := e.State.FindRepository(ctx, e.Repo.CommonDir)
	if err != nil {
		return result, err
	}
	if registered.ID != e.Repository {
		return result, ErrInvalidRequest
	}
	branch := input.Branch
	if branch == "" {
		branch, err = e.Repo.CurrentBranch(ctx)
		if err != nil {
			return result, err
		}
	}
	var change record.Change
	var revision record.Revision
	var remoteHead record.ObjectID
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
	if input.Action == record.Amend && input.Branch == "" {
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
	if input.Title != "" {
		_, body, hasBody := strings.Cut(message, "\n")
		message = input.Title
		if hasBody {
			message += "\n" + body
		}
	}
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
	if input.ResolveBuild == nil {
		return result, ErrInvalidRequest
	}
	build, err := input.ResolveBuild(ctx, evaluation)
	if err != nil {
		return result, err
	}
	spec := record.JobSpec{KeepFailed: input.KeepFailed, Action: input.Action, Source: committed.Source(revision.Source.Base), Targets: targets, Destination: record.VerificationComplete, Verification: record.VerificationRequired, Build: build.Build, BuildRequirements: build.Requirements, IncludeDependents: input.IncludeDependents, TargetBuilds: build.TargetBuilds,
		Preparation: &record.PreparationSpec{SourceBranch: branch, Platform: input.Platform, Author: record.CommitIdentity{Name: author.Name, Email: author.Email}, VerificationProblem: build.Problem,
			Correction: &record.CorrectionSpec{Scope: scope, ChangeID: change.ID, RevisionID: revision.ID, Branch: branch, PreviousHead: committed.Head, RemoteHead: remoteHead, Candidate: candidate}}}
	if input.Publication != nil {
		if e.Publisher == nil {
			return result, fmt.Errorf("workflow: publisher required")
		}
		if err := e.Publisher.Preflight(ctx); err != nil {
			return result, fmt.Errorf("%w: %w", ErrPublicationIntake, err)
		}
		destination, err := e.Publisher.Destination(ctx, *input.Publication)
		if err != nil {
			return result, fmt.Errorf("%w: %w", ErrPublicationIntake, err)
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
