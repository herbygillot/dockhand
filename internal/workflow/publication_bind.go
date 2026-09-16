package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/git/changeset"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/verify"
)

type PublicationRequest struct {
	Target   string
	ChangeID record.ChangeID
	ID       record.RequestID
	Branch   string
	Options  publish.Options
}

// BindPublication freezes committed source, applicable evidence, and remote
// preconditions without adopting a branch. Submit owns durable adoption.
func (e *Engine) BindPublication(ctx context.Context, input PublicationRequest) (Request, error) {
	return e.bindPublication(ctx, input, true)
}

// PlanPublication produces the same publication intent without requiring a
// write credential. It is used only for previews that are never submitted.
func (e *Engine) PlanPublication(ctx context.Context, input PublicationRequest) (Request, error) {
	return e.bindPublication(ctx, input, false)
}

func (e *Engine) bindPublication(ctx context.Context, input PublicationRequest, authenticate bool) (Request, error) {
	if e == nil || e.State == nil || e.Repository == "" {
		return Request{}, ErrNoState
	}
	if e.Repo == nil || e.Publisher == nil {
		return Request{}, fmt.Errorf("workflow: publication requires Git and a publisher")
	}
	if !validToken(string(input.ID)) {
		return Request{}, ErrInvalidRequest
	}
	timeouts, err := e.Timeouts.defaults()
	if err != nil {
		return Request{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeouts.Publish)
	defer cancel()
	registered, err := e.State.FindRepository(ctx, e.Repo.CommonDir)
	if err != nil {
		return Request{}, err
	}
	if registered.ID != e.Repository {
		return Request{}, ErrInvalidRequest
	}
	var continuation *record.Change
	if input.Target != "" || input.ChangeID != "" {
		change, err := e.SelectContribution(ctx, ContributionSelector{Target: input.Target, Branch: input.Branch, ChangeID: input.ChangeID})
		if err != nil {
			return Request{}, err
		}
		if err := contributionPrepared(change); err != nil {
			return Request{}, err
		}
		if err := e.Repo.RequireCleanBranch(ctx, change.Branch); err != nil {
			return Request{}, err
		}
		input.Branch = change.Branch
		continuation = &change
	}
	if input.Branch == "" {
		input.Branch, err = e.Repo.CurrentBranch(ctx)
		if err != nil {
			return Request{}, err
		}
	}
	if authenticate {
		if err := e.Publisher.Preflight(ctx); err != nil {
			return Request{}, err
		}
	}
	snapshot, err := changeset.CaptureBranch(ctx, e.Repo, input.Branch)
	if err != nil {
		return Request{}, err
	}
	var change record.Change
	var revision record.Revision
	var evidence record.Attempt
	var associated *record.PullRequest
	source := snapshot.Source("")
	err = e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		var err error
		change, err = r.OpenChangeByBranch(ctx, input.Branch)
		if errors.Is(err, state.ErrNotFound) && continuation == nil {
			return nil
		}
		if err != nil {
			return err
		}
		if continuation != nil && (change.ID != continuation.ID || change.CurrentRevision != continuation.CurrentRevision) {
			return ErrStaleRevision
		}
		revision, err = r.Revision(ctx, change.CurrentRevision)
		if err != nil {
			return err
		}
		source.Base = revision.Source.Base
		if len(change.Targets) != 1 {
			return fmt.Errorf("%w: publish currently requires one tracked target", ErrInvalidRequest)
		}
		if change.PullRequestID != "" {
			pr, err := r.PullRequest(ctx, change.PullRequestID)
			if err != nil {
				return err
			}
			associated = &pr
		}
		return nil
	})
	if err != nil {
		return Request{}, err
	}
	query := state.VerificationQuery{Tree: source.Tree, Limit: 1}
	if change.ID == "" {
		source, query.Target.Portfile, err = e.Publisher.UntrackedSource(ctx, source)
		if err != nil {
			return Request{}, err
		}
		change.Branch = input.Branch
	} else {
		query.Target = change.Targets[0]
	}
	err = e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		candidates, err := r.VerificationCandidates(ctx, query)
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			return fmt.Errorf("%w: verify the committed contribution before publishing", publish.ErrPrecondition)
		}
		evidence = candidates[0]
		if change.ID == "" {
			change.Targets = []record.Target{evidence.Spec.Target}
		}
		wanted := record.BuildSpec{Branch: change.Branch, Source: source, Target: change.Targets[0], Config: evidence.Spec.Config}
		if verdict := verify.Applicable(wanted, evidence); !verdict.Matches {
			return fmt.Errorf("%w: %s", publish.ErrPrecondition, strings.Join(verdict.Reasons, "; "))
		}
		return publicationCoverage(ctx, r, evidence, revision.Scope)
	})
	if err != nil {
		return Request{}, err
	}
	publication, err := e.Publisher.Plan(ctx, change, source, evidence, associated, input.Options)
	if err != nil {
		return Request{}, err
	}
	if err := e.describePublicationCoverage(ctx, &publication); err != nil {
		return Request{}, err
	}
	current, err := changeset.CaptureBranch(ctx, e.Repo, input.Branch)
	if err != nil {
		return Request{}, err
	}
	if current.Commit != snapshot.Commit || current.Tree != snapshot.Tree {
		return Request{}, fmt.Errorf("%w: branch %s changed while planning publication; run publish again", ErrStaleRevision, input.Branch)
	}
	spec, err := normalizeSpec(record.JobSpec{Action: record.Publish, Source: source, Targets: change.Targets, Build: &evidence.Spec.Config, Verification: record.VerificationRequired, Destination: record.Published, Publication: &publication})
	if err != nil {
		return Request{}, err
	}
	return Request{ID: input.ID, Spec: spec, Branch: &BranchInput{Scope: revision.Scope, Name: input.Branch, ExpectedChange: change.ID, ExpectedRevision: revision.ID}}, nil
}
