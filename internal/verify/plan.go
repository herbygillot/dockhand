package verify

import (
	"context"

	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/record"
)

type RevisionBump struct {
	Target record.Target
	Reason string
}

type Impact struct {
	Bumps        []RevisionBump
	Verification record.VerificationPlan
}

type Planner struct{ Ports macports.Reader }

func (p *Planner) Plan(ctx context.Context, revision record.Revision, targets []record.Target) (record.VerificationPlan, error) {
	return record.VerificationPlan{}, ErrNotImplemented
}

func (p *Planner) Dependents(ctx context.Context, revision record.Revision, evidence record.Evidence) (Impact, error) {
	return Impact{}, ErrNotImplemented
}
