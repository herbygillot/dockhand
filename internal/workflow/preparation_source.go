package workflow

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/v2/internal/git"
	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/publish"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

type PreparationRequest struct {
	Action              record.Action
	Version             string
	ID                  record.RequestID
	Branch              string
	Selection           macports.Selection
	Destination         record.Destination
	Verification        record.VerificationPolicy
	Build               *record.BuildConfig
	Author              record.CommitIdentity
	Platform            record.Platform
	Reason              string
	VerificationProblem string
	Publication         publish.Options
}

type BoundPreparation struct {
	Request    Request
	Evaluation macports.Snapshot
}

func (e *Engine) BindPreparation(ctx context.Context, request PreparationRequest) (BoundPreparation, error) {
	if e == nil || e.State == nil || e.Repository == "" {
		return BoundPreparation{}, ErrNoState
	}
	if e.Repo == nil || e.Ports == nil {
		return BoundPreparation{}, fmt.Errorf("workflow: preparation binding requires Git and MacPorts")
	}
	if !validToken(string(request.ID)) || !git.ValidBranchName(request.Branch) {
		return BoundPreparation{}, ErrInvalidRequest
	}
	registered, err := e.State.FindRepository(ctx, e.Repo.CommonDir)
	if err != nil {
		return BoundPreparation{}, err
	}
	if registered.ID != e.Repository {
		return BoundPreparation{}, fmt.Errorf("%w: preparation repository does not match state scope", ErrInvalidRequest)
	}
	source, targets, evaluation, err := e.bindBranchSource(ctx, request.Branch, request.Selection, request.Platform, "")
	if err != nil {
		return BoundPreparation{}, err
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
	spec, err := normalizeSpec(record.JobSpec{
		Action: request.Action, PublishTo: destination, Version: request.Version, Source: source, Targets: targets, Destination: request.Destination, Verification: request.Verification, Build: request.Build, Reason: request.Reason,
		Preparation: &record.PreparationSpec{SourceBranch: request.Branch, Platform: request.Platform, Author: request.Author, VerificationProblem: request.VerificationProblem},
	})
	if err != nil {
		return BoundPreparation{}, err
	}
	return BoundPreparation{Request: Request{ID: request.ID, Spec: spec}, Evaluation: evaluation}, nil
}
