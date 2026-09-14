package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/v2/internal/publish"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/state"
	"github.com/herbygillot/dockhand/v2/internal/verify"
)

type PublicationRequest struct {
	ID      record.RequestID
	Branch  string
	Options publish.Options
}

// BindPublication freezes committed source, applicable evidence, and remote
// preconditions without adopting a branch. Submit owns durable adoption.
func (e *Engine) BindPublication(ctx context.Context, input PublicationRequest) (Request, error) {
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
	if input.Branch == "" {
		input.Branch, err = e.Repo.CurrentBranch(ctx)
		if err != nil {
			return Request{}, err
		}
	}
	commit, tree, err := e.Repo.Branch(ctx, input.Branch)
	if err != nil {
		return Request{}, err
	}
	var change record.Change
	var revision record.Revision
	var evidence record.Attempt
	var associated *record.PullRequest
	source := record.Source{Commit: record.ObjectID(commit), Tree: record.ObjectID(tree)}
	err = e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		var err error
		change, err = r.OpenChangeByBranch(ctx, input.Branch)
		if errors.Is(err, state.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
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
		wanted := record.BuildSpec{Source: source, Target: change.Targets[0], Config: evidence.Spec.Config}
		if verdict := verify.Applicable(wanted, evidence); !verdict.Matches {
			return fmt.Errorf("%w: %s", publish.ErrPrecondition, strings.Join(verdict.Reasons, "; "))
		}
		return nil
	})
	if err != nil {
		return Request{}, err
	}
	publication, err := e.Publisher.Plan(ctx, change, source, evidence, associated, input.Options)
	if err != nil {
		return Request{}, err
	}
	currentCommit, currentTree, err := e.Repo.Branch(ctx, input.Branch)
	if err != nil {
		return Request{}, err
	}
	if currentCommit != commit || currentTree != tree {
		return Request{}, fmt.Errorf("%w: branch %s changed while planning publication; run publish again", ErrStaleRevision, input.Branch)
	}
	spec, err := normalizeSpec(record.JobSpec{Action: record.Publish, Source: source, Targets: change.Targets, Build: &evidence.Spec.Config, Verification: record.VerificationRequired, Destination: record.Published, Publication: &publication})
	if err != nil {
		return Request{}, err
	}
	return Request{ID: input.ID, Spec: spec, Branch: &BranchInput{Name: input.Branch, ExpectedChange: change.ID, ExpectedRevision: revision.ID}}, nil
}

func publicationEvidence(ctx context.Context, r state.Reader, job record.Job) error {
	if job.Spec.Publication == nil || job.Spec.Build == nil || len(job.Spec.Targets) != 1 {
		return ErrInvalidRequest
	}
	change, err := r.Change(ctx, job.ChangeID)
	if err != nil {
		return err
	}
	if len(change.Targets) != 1 || targetKey(change.Targets[0]) != targetKey(job.Spec.Targets[0]) {
		return fmt.Errorf("%w: publication must cover the tracked contribution target", publish.ErrPrecondition)
	}
	candidate, err := r.Attempt(ctx, job.Spec.Publication.EvidenceAttempt)
	if err != nil {
		return err
	}
	build := record.BuildSpec{Source: job.Spec.Source, Target: job.Spec.Targets[0], Config: *job.Spec.Build}
	if verdict := verify.Applicable(build, candidate); !verdict.Matches {
		return fmt.Errorf("%w: %s", publish.ErrPrecondition, strings.Join(verdict.Reasons, "; "))
	}
	latest, _, err := selectVerification(ctx, r, job, build)
	if err != nil {
		return err
	}
	if latest.ID == "" {
		return fmt.Errorf("%w: recorded verification is no longer applicable; verify again", publish.ErrPrecondition)
	}
	return nil
}
