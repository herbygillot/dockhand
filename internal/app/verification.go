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
)

type Verification struct {
	KeepFailed        bool
	WorkingTree       bool
	ChangeID          record.ChangeID
	UseRecordedBuild  bool
	IncludeDependents bool
	Fresh             bool
	ID                record.RequestID
	Branch            string
	Selection         macports.Selection
	Tests             record.TestPolicy
	FromSource        bool
}

func (s *Services) BindVerification(ctx context.Context, request Verification) (workflow.BoundVerification, error) {
	if s.providerName == "auto" {
		return workflow.BoundVerification{}, fmt.Errorf("automatic provider selection is supported for bumps; choose tart or github for verify")
	}
	if s.providerName == verify.ProviderGitHub && (request.WorkingTree || request.Fresh) {
		return workflow.BoundVerification{}, fmt.Errorf("github verification requires committed source; --fresh is unsupported, rerun the workflow on GitHub and verify again")
	}
	if s.providerName == verify.ProviderGitHub {
		request.Fresh = true
	}
	platform, err := s.ports.NativePlatform(ctx)
	if err != nil {
		return workflow.BoundVerification{}, err
	}
	var continuation *workflow.ContributionSelector
	if !request.WorkingTree && (request.Branch == "" || request.ChangeID != "") {
		selected := workflow.ContributionSelector{Target: request.Selection.Selector, Branch: request.Branch, ChangeID: request.ChangeID}
		if selected.Target == "" && selected.Branch == "" && selected.ChangeID == "" {
			selected.Branch, err = s.Workflow.Repo.CurrentBranch(ctx)
			if err != nil {
				return workflow.BoundVerification{}, err
			}
		}
		continuation = &selected
	}
	if !request.WorkingTree && continuation == nil && request.Branch != "" && macports.ValidName(request.Selection.Selector) {
		change, lookupErr := s.Workflow.SelectContribution(ctx, workflow.ContributionSelector{Branch: request.Branch})
		if lookupErr != nil && !errors.Is(lookupErr, state.ErrNotFound) {
			return workflow.BoundVerification{}, lookupErr
		}
		if lookupErr == nil && change.InitiatingTarget != "" {
			continuation = &workflow.ContributionSelector{Target: request.Selection.Selector, Branch: request.Branch}
		}
	}
	return s.Workflow.BindVerification(ctx, workflow.VerificationRequest{KeepFailed: request.KeepFailed,
		Continue: continuation, UseRecordedBuild: request.UseRecordedBuild, IncludeDependents: request.IncludeDependents, ID: request.ID, Branch: request.Branch, Selection: request.Selection, Platform: platform, Fresh: request.Fresh,
		ResolveBuild: s.buildResolver(platform, request.Tests, request.FromSource, false),
	})
}
