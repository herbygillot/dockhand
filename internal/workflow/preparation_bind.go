package workflow

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
)

type PreparationRequest struct {
	// EditIntent is the person's choices; binding resolves a fresh stub
	// selection into it, and a continued contribution carries its prior one.
	record.EditIntent
	AllSubports         bool
	KeepFailed          bool
	ChangeID            record.ChangeID
	TargetBuilds        map[string]record.BuildConfig
	IncludeDependents   bool
	Action              record.Action
	Version             string
	ID                  record.RequestID
	Source              record.Source
	SourceBranch        string
	SourceURL           string
	Selection           macports.Selection
	Destination         record.Destination
	Verification        record.VerificationPolicy
	Build               *record.BuildConfig
	BuildRequirements   *record.BuildRequirements
	Author              record.CommitIdentity
	Platform            record.Platform
	Reason              string
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
	source := request.Source
	if source.Base != source.Commit {
		return BoundPreparation{}, ErrInvalidRequest
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(source.Commit)})
	if err != nil {
		return BoundPreparation{}, err
	}
	if trees[string(source.Commit)] != string(source.Tree) {
		return BoundPreparation{}, fmt.Errorf("%w: preparation commit/tree mismatch", ErrInvalidRequest)
	}
	targets, evaluation, err := e.bindSnapshot(ctx, source, request.Selection, request.Platform, nil)
	if err != nil {
		return BoundPreparation{}, err
	}
	// A stub such as py-foo cannot be edited or built itself; the bump lands
	// on its newest versioned subport as a shared release, and the person's
	// name stays on the contribution.
	if carrier, stub := macports.ResolveStub(evaluation, targets[0]); stub != "" {
		targets = []record.Target{carrier}
		request.SharedRelease, request.Stub = true, stub
		progress.Report(ctx, "%s is a stub; editing %s and its sibling subports as one release", stub, carrier.Name)
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
		resolved, err := e.publicationDestination(ctx, request.Publication)
		if err != nil {
			return BoundPreparation{}, err
		}
		destination = &resolved
	}
	source.Base = source.Commit
	evaluation.Source = source
	spec, err := normalizeSpec(record.JobSpec{KeepFailed: request.KeepFailed,
		ChangeID: request.ChangeID, TargetBuilds: request.TargetBuilds, IncludeDependents: request.IncludeDependents, AllSubports: request.AllSubports, Action: request.Action, PublishTo: destination, Version: request.Version, Source: source, Targets: targets, EvaluatedVersions: evaluatedVersions(evaluation, targets), Destination: request.Destination, Verification: request.Verification, Build: request.Build, BuildRequirements: request.BuildRequirements, Reason: request.Reason,
		Preparation: &record.PreparationSpec{EditIntent: request.EditIntent, SourceBranch: request.SourceBranch, SourceURL: request.SourceURL, Platform: request.Platform, Author: request.Author, VerificationProblem: request.VerificationProblem},
	})
	if err != nil {
		return BoundPreparation{}, err
	}
	return BoundPreparation{Request: Request{ID: request.ID, Spec: spec}, Evaluation: evaluation}, nil
}
