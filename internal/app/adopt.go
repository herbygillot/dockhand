package app

import (
	"context"

	"github.com/herbygillot/dockhand/internal/workflow"
)

// AdoptRequest names a branch a person prepared, to be tracked as a contribution.
type AdoptRequest struct {
	Branch string
	// Target names the port when the changed directory's main port is not
	// the one meant; empty infers it from the directory.
	Target string
	DryRun bool
	Squash bool
}

// Adopt fetches master, so the branch's base can be checked against it, and
// records the branch as a contribution, or with DryRun only says what it
// would record.
func (s *Services) Adopt(ctx context.Context, request AdoptRequest) (workflow.AdoptResult, error) {
	platform, err := s.ports.NativePlatform(ctx)
	if err != nil {
		return workflow.AdoptResult{}, err
	}
	master, err := preparationSource(ctx, s.Workflow.Repo)
	if err != nil {
		return workflow.AdoptResult{}, err
	}
	return s.Workflow.AdoptContribution(ctx, workflow.AdoptRequest{Branch: request.Branch, Target: request.Target, Upstream: master.Commit, Platform: platform, DryRun: request.DryRun, Squash: request.Squash})
}
