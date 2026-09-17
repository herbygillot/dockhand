package workflow

import (
	"context"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow/policy"
)

// ErrInvalidRequest means a request violates the intake contract.
var ErrInvalidRequest = policy.ErrInvalidRequest

func (e *Engine) describePublicationCoverage(ctx context.Context, spec *record.PublicationSpec) error {
	if spec.ExpectedPR != nil {
		return nil
	}
	return e.State.View(ctx, e.Repository, func(ctx context.Context, r state.Reader) error {
		return policy.DescribeCoverage(ctx, r, spec)
	})
}
