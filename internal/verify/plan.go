package verify

import (
	"context"

	"github.com/herbygillot/dockhand/v2/internal/macports"
	"github.com/herbygillot/dockhand/v2/internal/model"
)

type RevisionBump struct {
	Target model.Target
	Reason string
}

type Impact struct {
	Bumps        []RevisionBump
	Verification model.VerificationPlan
}

type Planner struct{ Ports macports.Reader }

func (p *Planner) Plan(ctx context.Context, revision model.Revision, targets []model.Target) (model.VerificationPlan, error) {
	return model.VerificationPlan{}, ErrNotImplemented
}

func (p *Planner) Dependents(ctx context.Context, revision model.Revision, evidence model.Evidence) (Impact, error) {
	return Impact{}, ErrNotImplemented
}

func Judge(observation Observation) (model.Evidence, error) {
	return model.Evidence{Verdict: model.VerdictUnknown}, ErrNotImplemented
}
