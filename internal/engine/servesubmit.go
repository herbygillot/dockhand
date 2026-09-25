package engine

import (
	"context"
	"fmt"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// ServeCandidate is a branch serve prepared whose check passed, and what
// submitting it would do, or why it is held for a person's look (Design
// v3 §11's guardrails).
type ServeCandidate struct {
	Branch model.Branch
	Plan   SubmitPlan
	// Held are the reasons it waits for a person; empty when serve may
	// submit it.
	Held []string
}

// ServeNote closes the description of a pull request serve opens by
// itself.
const ServeNote = "Opened by `dockhand serve` for an update it prepared and checked, without a person's review."

// ServeCandidates are the open branches serve itself started, with no
// pull request yet, whose latest check passed for exactly their committed
// files. Each is planned as submit would plan it, and held when anything
// asks for a person: a publication rule the check doesn't meet without an
// acknowledgement, a finding in the upstream comparison, or a commit-rule
// finding.
func (e *Engine) ServeCandidates(ctx context.Context) ([]ServeCandidate, error) {
	statuses, err := e.Status(ctx)
	if err != nil {
		return nil, err
	}
	var candidates []ServeCandidate
	for _, status := range statuses {
		branch := status.Branch
		if branch.Origin != model.OriginServe || branch.PullRequest != nil || status.Missing || status.Commits == 0 ||
			status.Latest == nil || status.Latest.State != model.RunPassed || !status.Current || len(status.Edited) > 0 {
			continue
		}
		plan, err := e.PlanSubmit(ctx, SubmitRequest{Branch: branch})
		candidate := ServeCandidate{Branch: branch, Plan: plan}
		if err != nil {
			candidate.Held = append(candidate.Held, err.Error())
			candidates = append(candidates, candidate)
			continue
		}
		candidate.Held = append(candidate.Held, plan.Blocking...)
		candidate.Held = append(candidate.Held, status.Held...)
		for _, finding := range plan.Findings {
			candidate.Held = append(candidate.Held, "commit rules: "+finding.String())
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

// SubmitForServe opens the pull request for a candidate serve may submit,
// saying in its description that no person reviewed it.
func (e *Engine) SubmitForServe(ctx context.Context, candidate ServeCandidate) (Submitted, error) {
	if len(candidate.Held) > 0 {
		return Submitted{}, fmt.Errorf("%s is held for a look: %s", candidate.Branch.ShortName(), candidate.Held[0])
	}
	plan := candidate.Plan
	plan.Body = plan.Body + "\n\n" + ServeNote + "\n"
	submitted, err := e.ApplySubmit(ctx, plan)
	if err != nil {
		return submitted, err
	}
	err = e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: candidate.Branch.ID, Kind: "serve.submit", Level: model.LevelInfo,
			Message: fmt.Sprintf("serve opened #%d for %s, which passed its check", submitted.PullRequest.Ref.Number, candidate.Branch.ShortName())})
		return err
	})
	return submitted, err
}
