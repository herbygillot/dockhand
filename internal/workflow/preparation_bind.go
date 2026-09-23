package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
)

type PreparationRequest struct {
	// Resolution is what the selection resolved to: the source the edit
	// runs on, the contribution it lands on, the target, and the intent,
	// subject, and references with a prior job's inherited. Binding
	// resolves a fresh stub selection into the intent it records.
	Resolution        Resolution
	AllSubports       bool
	KeepFailed        bool
	TargetBuilds      map[string]record.BuildConfig
	IncludeDependents bool
	Action            record.Action
	Version           string
	ID                record.RequestID
	SourceBranch      string
	SourceURL         string
	Destination       record.Destination
	Verification      record.VerificationPolicy
	Build             *record.BuildConfig
	BuildRequirements *record.BuildRequirements
	// Author is filled by the binding when empty, from the repository.
	Author              record.CommitIdentity
	Platform            record.Platform
	VerificationProblem string
	Publication         publish.Options
	ResolveBuild        BuildResolver
}

type BoundPreparation struct {
	Request    Request
	Evaluation macports.Snapshot
}

func (e *Engine) BindPreparation(ctx context.Context, request PreparationRequest) (BoundPreparation, error) {
	if e == nil || e.State == nil || e.Repository == "" {
		return BoundPreparation{}, errNoState
	}
	if e.Repo == nil || e.Ports == nil {
		return BoundPreparation{}, fmt.Errorf("workflow: preparation binding requires Git and MacPorts")
	}
	if !validToken(string(request.ID)) || !git.ValidBranchName(request.SourceBranch) {
		return BoundPreparation{}, ErrInvalidRequest
	}
	if err := e.requireRepository(ctx, "preparation repository does not match state scope"); err != nil {
		return BoundPreparation{}, err
	}
	resolution := request.Resolution
	source := resolution.Source
	intent, subject, references, selection := resolution.Intent, resolution.Subject, resolution.References, resolution.Selection
	var target *contribution
	switch resolution.Kind {
	case Fresh, Continue:
		if source.Base != source.Commit {
			return BoundPreparation{}, ErrInvalidRequest
		}
	case Onto, Adopt:
		if resolution.Change == nil || resolution.Revision == nil {
			return BoundPreparation{}, ErrInvalidRequest
		}
		// The binding reconfirms what integration will check: the
		// revision is current, no other job is pending on the
		// contribution, and an attached pull request is open.
		bound, err := e.bindContribution(ctx, resolution.Change.ID)
		if err != nil {
			return BoundPreparation{}, err
		}
		if bound.revision.ID != resolution.Revision.ID {
			return BoundPreparation{}, ErrStaleRevision
		}
		if _, err := bound.selection(nil); err != nil {
			return BoundPreparation{}, err
		}
		if request.Destination == record.Published {
			if err := bound.allowsPublication(&request.Publication); err != nil {
				return BoundPreparation{}, err
			}
		}
		target = &bound
	default:
		return BoundPreparation{}, fmt.Errorf("%w: unresolved selection", ErrInvalidRequest)
	}
	if request.Author == (record.CommitIdentity{}) {
		author, err := e.Repo.Author(ctx)
		if err != nil {
			return BoundPreparation{}, err
		}
		request.Author = record.CommitIdentity{Name: author.Name, Email: author.Email}
	}
	// A revision bump needs a subject saying why; an update onto a
	// contribution, or one continuing a job, has taken its own by now.
	if request.Action == record.BumpRevision && strings.TrimSpace(subject) == "" {
		return BoundPreparation{}, fmt.Errorf("%w: a revision bump needs a subject saying why; --subject \"revbump for simdutf update\" is what maintainers read", ErrInvalidRequest)
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(source.Commit)})
	if err != nil {
		return BoundPreparation{}, err
	}
	if trees[string(source.Commit)] != string(source.Tree) {
		return BoundPreparation{}, fmt.Errorf("%w: preparation commit/tree mismatch", ErrInvalidRequest)
	}
	targets, evaluation, err := e.bindSnapshot(ctx, source, selection, request.Platform, nil)
	if err != nil {
		return BoundPreparation{}, err
	}
	// A stub such as py-foo cannot be edited or built itself; the bump lands
	// on its newest versioned subport as a shared release, and the person's
	// name stays on the contribution. The stub is recorded on the intent for
	// every action, so the editor keeps its name and its livecheck; the
	// shared-release authorization is a bump's alone, since only a bump
	// moves siblings, and the editor authorizes a recorded stub's family
	// itself.
	if carrier, stub := macports.ResolveStub(evaluation, targets[0]); stub != "" {
		targets = []record.Target{carrier}
		intent.SharedRelease, intent.Stub = request.Action == record.Bump, stub
		progress.Report(ctx, "%s is a stub; editing %s and its sibling subports as one release", stub, carrier.Name)
	} else if stub := macports.StubOf(evaluation, targets[0].Name); target != nil && stub != "" {
		// An update onto a contribution names the contribution's target,
		// the carrier a fresh bump chose or a hand-made branch edited; the
		// release it edits is still the stub's, shared by every member.
		intent.SharedRelease, intent.Stub = request.Action == record.Bump, stub
		progress.Report(ctx, "%s carries %s's release; editing it and its sibling subports as one release", targets[0].Name, stub)
	} else if request.Action == record.Bump && targets[0].Subport == "" {
		// A main port's release is its Portfile's: the subports sharing its
		// version move with it by construction, and the person reviews the
		// whole edit. The authorization is recorded with the job, as a
		// stub's is; only a named subport needs --shared-release.
		intent.SharedRelease = true
	}
	if request.ResolveBuild != nil {
		if request.Build != nil || request.BuildRequirements != nil || request.VerificationProblem != "" || request.Verification != record.VerificationRequired {
			return BoundPreparation{}, fmt.Errorf("%w: build selection is inconsistent", ErrInvalidRequest)
		}
		resolved, resolveErr := request.ResolveBuild(ctx, evaluation)
		if resolveErr != nil {
			return BoundPreparation{}, resolveErr
		}
		request.TargetBuilds = resolved.TargetBuilds
		request.Build, request.BuildRequirements, request.VerificationProblem = resolved.Build, resolved.Requirements, resolved.Problem
	}
	var destination *record.PublicationDestination
	if request.Destination == record.Published {
		// An update onto a contribution with a pull request publishes
		// where the pull request is, whoever owns its head.
		resolved, err := e.destination(ctx, target, request.Publication)
		if err != nil {
			return BoundPreparation{}, err
		}
		destination = &resolved
	}
	if target == nil {
		source.Base = source.Commit
	}
	evaluation.Source = source
	spec, err := normalizeSpec(record.JobSpec{KeepFailed: request.KeepFailed,
		ChangeID: resolution.ChangeID(), TargetBuilds: request.TargetBuilds, IncludeDependents: request.IncludeDependents, AllSubports: request.AllSubports, Action: request.Action, PublishTo: destination, Version: request.Version, Source: source, Targets: targets, EvaluatedVersions: evaluatedVersions(evaluation, targets), Destination: request.Destination, Verification: request.Verification, Build: request.Build, BuildRequirements: request.BuildRequirements, Subject: subject, References: references,
		Preparation: &record.PreparationSpec{EditIntent: intent, SourceBranch: request.SourceBranch, SourceURL: request.SourceURL, Platform: request.Platform, Author: request.Author, VerificationProblem: request.VerificationProblem},
	})
	if err != nil {
		return BoundPreparation{}, err
	}
	if target != nil {
		correction := target.correction(target.revision.Source.Commit, record.Source{})
		spec.Preparation.Correction = &correction
	}
	return BoundPreparation{Request: Request{ID: request.ID, Spec: spec}, Evaluation: evaluation}, nil
}
