package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/herbygillot/dockhand/internal/verify"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow"
	"github.com/herbygillot/dockhand/internal/workflow/choice"
)

type Verification struct {
	KeepFailed        bool
	WorkingTree       bool
	ChangeID          record.ChangeID
	UseRecordedBuild  bool
	IncludeDependents bool
	AllSubports       bool
	Fresh             bool
	ID                record.RequestID
	// Branch selects a tracked contribution's branch, or with Adopt names a
	// branch dockhand did not make whose committed contents are verified.
	Branch     string
	Adopt      bool
	Selection  macports.Selection
	Tests      record.TestPolicy
	FromSource bool
	// OS names the macOS releases to build on, by setup's names or major
	// versions, or AvailablePlatforms for every prepared image; empty builds
	// on the evaluated platform.
	OS []string
}

func (s *Services) BindVerification(ctx context.Context, request Verification) (workflow.BoundVerification, error) {
	if s.providerName == verify.ProviderGitHub && (request.WorkingTree || request.Fresh) {
		return workflow.BoundVerification{}, fmt.Errorf("github verification requires committed source; --fresh is unsupported, rerun the workflow on GitHub and verify again")
	}
	if s.providerName == verify.ProviderGitHub {
		request.Fresh = true
	}
	if s.providerName == verify.ProviderGitHub && len(request.OS) > 0 {
		return workflow.BoundVerification{}, fmt.Errorf("--os selects Tart images; GitHub verification builds on the fork workflow's runner matrix, which dockhand does not choose")
	}
	platform, err := s.ports.NativePlatform(ctx)
	if err != nil {
		return workflow.BoundVerification{}, err
	}
	platforms, err := s.buildPlatforms(ctx, platform, request.OS)
	if err != nil {
		return workflow.BoundVerification{}, err
	}
	var continuation *workflow.Resolution
	if !request.WorkingTree && !request.Adopt {
		selection := workflow.ResolutionRequest{Action: record.Verify, Selection: request.Selection, Branch: request.Branch, ChangeID: request.ChangeID, Platform: platform}
		if selection.Selection.Selector == "" && selection.Branch == "" && selection.ChangeID == "" {
			selection.Branch, err = s.Workflow.Repo.CurrentBranch(ctx)
			if err != nil {
				return workflow.BoundVerification{}, err
			}
		}
		resolution, err := s.Workflow.Resolve(ctx, selection)
		if err != nil {
			return workflow.BoundVerification{}, err
		}
		continuation = &resolution
	}
	// An adopted branch that turns out to be tracked is continued as itself.
	if !request.WorkingTree && request.Adopt && request.Branch != "" && macports.ValidName(request.Selection.Selector) {
		resolution, lookupErr := s.Workflow.Resolve(ctx, workflow.ResolutionRequest{Action: record.Verify, Selection: request.Selection, Branch: request.Branch, Platform: platform})
		if lookupErr != nil && !errors.Is(lookupErr, state.ErrNotFound) {
			return workflow.BoundVerification{}, lookupErr
		}
		if lookupErr == nil && resolution.Change.InitiatingTarget != "" {
			continuation = &resolution
		}
	}
	bound, err := s.Workflow.BindVerification(ctx, workflow.VerificationRequest{KeepFailed: request.KeepFailed,
		Tracked: continuation, UseRecordedBuild: request.UseRecordedBuild, IncludeDependents: request.IncludeDependents, AllSubports: request.AllSubports, ID: request.ID, Branch: request.Branch, Selection: request.Selection, Platform: platform, Platforms: platforms, Fresh: request.Fresh,
		ResolveBuild: s.providers.Resolver(platform, choice.Options{Tests: request.Tests, FromSource: request.FromSource, Platforms: platforms}),
	})
	if errors.Is(err, state.ErrNotFound) && request.Branch != "" && !request.Adopt && !request.WorkingTree {
		return bound, fmt.Errorf("%w; --adopt %s verifies that branch's committed contents", err, request.Branch)
	}
	return bound, err
}
