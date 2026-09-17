package workflow

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/publish"
	"github.com/herbygillot/dockhand/internal/record"
)

type PreparationRequest struct {
	SharedRelease       bool
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
	registered, err := e.State.FindRepository(ctx, e.Repo.CommonDir)
	if err != nil {
		return BoundPreparation{}, err
	}
	if registered.ID != e.Repository {
		return BoundPreparation{}, fmt.Errorf("%w: preparation repository does not match state scope", ErrInvalidRequest)
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
		if e.Publisher == nil {
			return BoundPreparation{}, fmt.Errorf("workflow: publisher required")
		}
		timeouts, err := e.Timeouts.defaults()
		if err != nil {
			return BoundPreparation{}, err
		}
		call, cancel := context.WithTimeout(ctx, timeouts.Publish)
		if err := e.Publisher.Preflight(call); err != nil {
			cancel()
			return BoundPreparation{}, err
		}
		resolved, err := e.Publisher.Destination(call, request.Publication)
		cancel()
		if err != nil {
			return BoundPreparation{}, err
		}
		destination = &resolved
	}
	source.Base = source.Commit
	evaluation.Source = source
	spec, err := normalizeSpec(record.JobSpec{KeepFailed: request.KeepFailed,
		ChangeID: request.ChangeID, TargetBuilds: request.TargetBuilds, IncludeDependents: request.IncludeDependents, Action: request.Action, PublishTo: destination, Version: request.Version, Source: source, Targets: targets, EvaluatedVersions: evaluatedVersions(evaluation, targets), Destination: request.Destination, Verification: request.Verification, Build: request.Build, BuildRequirements: request.BuildRequirements, Reason: request.Reason,
		Preparation: &record.PreparationSpec{SharedRelease: request.SharedRelease, SourceBranch: request.SourceBranch, SourceURL: request.SourceURL, Platform: request.Platform, Author: request.Author, VerificationProblem: request.VerificationProblem},
	})
	if err != nil {
		return BoundPreparation{}, err
	}
	return BoundPreparation{Request: Request{ID: request.ID, Spec: spec}, Evaluation: evaluation}, nil
}
