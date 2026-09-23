package workflow

import (
	"context"
	"errors"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/state"
	"github.com/herbygillot/dockhand/internal/workflow/policy"
)

// ErrInvalidRequest means a request violates the intake contract.
var ErrInvalidRequest = policy.ErrInvalidRequest

// ErrPublicationIntake marks a publication destination that could not be
// bound when the request was accepted: no forge login, no fork, or an
// ambiguous remote layout. Nothing has been prepared or built yet.
var ErrPublicationIntake = errors.New("workflow: publication cannot be bound")

func (e *Engine) describePublicationCoverage(ctx context.Context, spec *record.PublicationSpec) error {
	if spec.ExpectedPR != nil {
		return nil
	}
	return e.State.View(ctx, func(ctx context.Context, r state.Reader) error {
		return policy.DescribeCoverage(ctx, r, spec)
	})
}
