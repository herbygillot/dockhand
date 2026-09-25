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

// Passing is the open branches with checked work not yet on a pull
// request, sorted by whether it may be submitted as passing.
type Passing struct {
	// Ready are those whose latest check passed for exactly their
	// committed files, with nothing edited since.
	Ready []BranchStatus
	// Others counts those whose latest check didn't pass, or whose files
	// have changed since it ran.
	Others int
}

// PassingBranches is the one definition of a branch that passed and is
// ready to submit, for submit --passing and for serve: it has commits and
// a finished check, its pull request doesn't already have its head, and
// the checks of exactly its committed files built every changed target
// and passed, whatever --only narrowed.
func (e *Engine) PassingBranches(ctx context.Context) (Passing, error) {
	statuses, err := e.Status(ctx)
	if err != nil {
		return Passing{}, err
	}
	var passing Passing
	for _, status := range statuses {
		switch {
		case status.Missing || status.Commits == 0 || status.Latest == nil || status.Pushed():
		case status.Latest.State == model.RunPassed && status.Current && len(status.Edited) == 0 && status.Evidence != nil && len(status.Evidence.Failed()) == 0:
			passing.Ready = append(passing.Ready, status)
		default:
			passing.Others++
		}
	}
	return passing, nil
}

// ServeNote closes the description of a pull request serve opens by
// itself.
const ServeNote = "Opened by `dockhand serve` for an update it prepared and checked, without a person's review."

// ServeCandidates are the passing branches (PassingBranches) serve itself
// started, with no pull request yet. Each is planned as submit would plan
// it, and held when anything asks for a person: a publication rule the
// check doesn't meet without an acknowledgement, a finding in the
// upstream comparison, or a commit-rule finding.
func (e *Engine) ServeCandidates(ctx context.Context) ([]ServeCandidate, error) {
	passing, err := e.PassingBranches(ctx)
	if err != nil {
		return nil, err
	}
	var candidates []ServeCandidate
	for _, status := range passing.Ready {
		branch := status.Branch
		if branch.Origin != model.OriginServe || branch.PullRequest != nil {
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
		_, err := tx.AppendEvent(model.Event{At: e.now(), Branch: candidate.Branch.ID, Kind: ServeSubmitKind, Level: model.LevelInfo,
			Message: fmt.Sprintf("serve opened #%d for %s, which passed its check", submitted.PullRequest.Ref.Number, candidate.Branch.ShortName())})
		return err
	})
	return submitted, err
}
