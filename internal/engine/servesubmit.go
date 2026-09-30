package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"

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
// it, and held when anything asks for a person (SubmitPlan.held).
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
		candidate.Held = plan.held()
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

// Held are the reasons a submission no person looked over waits for one,
// as dockhand bump's does (SubmitPlan.held), for a branch of any origin.
func (e *Engine) Held(_ context.Context, plan SubmitPlan) ([]string, error) {
	return plan.held(), nil
}

// held are the reasons a submission no person looked over waits for one
// (Design v3 §11's guardrails): what blocks it, and its concerns.
func (p SubmitPlan) held() []string {
	held := slices.Clone(p.Blocking)
	for _, concern := range p.Concerns() {
		held = append(held, concern.Detail)
	}
	return held
}

// Concerns are what a submission would wait on a person's look for, were
// nobody to look it over (the assessment design, E), each once: what its
// assessments found that a passing build can't catch, or couldn't check
// (D4); a commit-rule finding; and another open pull request for its
// ports, or not knowing whether there is one. A person's own submission
// shows them and goes ahead, and each kind of submission keeps its own
// rules over its evidence (§3's publication rule).
func (p SubmitPlan) Concerns() []model.Concern {
	var concerns []model.Concern
	add := func(concern model.Concern) {
		if !slices.ContainsFunc(concerns, func(c model.Concern) bool { return c.Key() == concern.Key() }) {
			concerns = append(concerns, concern)
		}
	}
	for _, found := range p.Upstream {
		for _, change := range found.Comparison.Changes {
			if change.Hold {
				add(model.Concern{Origin: model.FromUpstream, Port: found.Port, Rule: change.Rule, Path: change.Path, Subject: change.Subject, Class: change.Class, Detail: change.Message})
			}
		}
		if problem := found.Comparison.Problem; problem != "" {
			add(model.Concern{Origin: model.FromUpstream, Port: found.Port, Rule: "not-compared", Class: model.UnknownBaseline,
				Detail: "the upstream archives couldn't be compared: " + problem})
		}
	}
	for _, finding := range p.Findings {
		add(model.Concern{Origin: model.FromCommitRules, Rule: finding.Code, Detail: "commit rules: " + finding.String()})
	}
	for _, pr := range p.Others {
		add(model.Concern{Origin: model.FromOtherPullRequests, Rule: "open", Subject: fmt.Sprint(pr.Number), Detail: fmt.Sprintf("#%d is open for the same port: %s", pr.Number, pr.Title)})
	}
	if p.SearchProblem != "" {
		add(model.Concern{Origin: model.FromOtherPullRequests, Rule: "search-failed", Detail: "couldn't look for other open pull requests: " + p.SearchProblem})
	}
	return concerns
}

// assessedHolds are why the recorded assessments of a branch's files hold
// it for a person's look (UpstreamComparison.Holds), and how far they're
// recorded: status reads them, and collects nothing.
func (e *Engine) assessedHolds(ctx context.Context, branch model.Branch, tree model.ObjectID, directories []string) ([]string, AssessmentState, error) {
	assessments, err := e.revisionAssessments(ctx, branch.ID, branch.Base, tree, false)
	if err != nil {
		return nil, "", err
	}
	slices.SortFunc(assessments, func(a, b model.Assessment) int { return strings.Compare(a.Port, b.Port) })
	var held []string
	state := AssessmentAvailable
	for _, a := range assessments {
		held = append(held, a.Comparison.Holds()...)
		if a.Comparison.Problem != "" {
			state = AssessmentIncomplete
		}
	}
	for _, directory := range directories {
		if !slices.ContainsFunc(assessments, func(a model.Assessment) bool { return a.Directory == directory }) {
			state = AssessmentPending
		}
	}
	return held, state, nil
}

// AssessmentState is how far a branch's files are assessed, as status
// reads what's recorded: every port's assessment recorded, one of them
// incomplete, or some not yet recorded, which a check or a submission
// collects.
type AssessmentState string

const (
	AssessmentAvailable  AssessmentState = "available"
	AssessmentIncomplete AssessmentState = "incomplete"
	AssessmentPending    AssessmentState = "pending"
)

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
