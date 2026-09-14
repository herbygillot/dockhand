package app

import (
	"context"

	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/record"
	"github.com/herbygillot/dockhand/v2/internal/workflow"
)

type Verification struct {
	Fresh      bool
	ID         record.RequestID
	Branch     string
	Selection  macports.Selection
	Tests      record.TestPolicy
	FromSource bool
}

func (s *Services) BindVerification(ctx context.Context, request Verification) (workflow.BoundVerification, error) {
	platform, err := s.ports.NativePlatform(ctx)
	if err != nil {
		return workflow.BoundVerification{}, err
	}
	return s.Workflow.BindVerification(ctx, workflow.VerificationRequest{
		ID: request.ID, Branch: request.Branch, Selection: request.Selection, Platform: platform, Fresh: request.Fresh,
		ResolveBuild: s.buildResolver(platform, request.Tests, request.FromSource, false),
	})
}
