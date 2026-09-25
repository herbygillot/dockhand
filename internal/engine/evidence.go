package engine

import (
	"context"
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// TargetEvidence is one planned target's result across a run's
// environments.
type TargetEvidence struct {
	Target model.PlanTarget
	// Outcomes are the target's result in each environment, in the plan's
	// order; OutcomeNotRun where none was recorded.
	Outcomes []model.TargetResult
	Passed   bool
	// Unchecked is true when no check of the files built the target in an
	// environment it is required in: --only left it out, or the check
	// stopped before it.
	Unchecked bool
}

// Evidence is what the finished checks of a tree established, judged
// against everything the newest of them required: its own targets and
// the changed targets its --only left out.
type Evidence struct {
	Run     model.Run
	Plan    model.Plan
	Targets []TargetEvidence
	// Earlier are older checks of the same files whose results fill in what
	// Run didn't build. A result holds for its tree, so a narrowed check
	// after a full one keeps the full one's results.
	Earlier []model.Run
}

// Unchecked lists the targets no check of the files built everywhere they
// are required.
func (e Evidence) Unchecked() []TargetEvidence {
	var unchecked []TargetEvidence
	for _, target := range e.Targets {
		if target.Unchecked {
			unchecked = append(unchecked, target)
		}
	}
	return unchecked
}

// Failed lists the targets that did not pass in every environment.
func (e Evidence) Failed() []TargetEvidence {
	var failed []TargetEvidence
	for _, target := range e.Targets {
		if !target.Passed {
			failed = append(failed, target)
		}
	}
	return failed
}

// Acceptable reports whether a failed target may be acknowledged with
// --accept: a revision-only target, or an extra from --also (Design v3 §3).
func Acceptable(target model.PlanTarget) bool {
	return target.Kind == model.RevisionOnly || target.Role == model.Also
}

// EvidenceFor finds what the finished checks of a branch's tree
// established (treeEvidence). A result holds for its tree, whichever
// snapshot or commit was checked, because a check builds files, not
// history. It reports false when no finished check covers the tree.
func (e *Engine) EvidenceFor(ctx context.Context, branch model.BranchID, tree model.ObjectID) (Evidence, bool, error) {
	var evidence Evidence
	found := false
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		runs, err := treeRuns(r, branch, tree)
		if err != nil || len(runs) == 0 {
			return err
		}
		evidence, err = treeEvidence(r, runs[0], runs)
		found = err == nil
		return err
	})
	return evidence, found, err
}

// treeRuns lists a branch's finished checks of a tree, newest first. A
// baseline is evidence about another run, never the branch's own check.
func treeRuns(r store.Reader, branch model.BranchID, tree model.ObjectID) ([]model.Run, error) {
	revisions, err := r.Revisions(branch)
	if err != nil {
		return nil, err
	}
	matching := map[model.RevisionID]bool{}
	for _, revision := range revisions {
		if revision.Source.Tree == tree {
			matching[revision.ID] = true
		}
	}
	if len(matching) == 0 {
		return nil, nil
	}
	runs, err := r.Runs(store.RunFilter{Branch: branch, States: []model.RunState{model.RunPassed, model.RunFailed, model.RunAttention}})
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(runs, func(run model.Run) bool { return !matching[run.Revision] || run.BaselineOf != "" }), nil
}

// treeEvidence is what a run established, judged against everything its
// plan required, the changed targets --only left out included, with the
// gaps filled from earlier checks of the same files, newest first. What
// no check built is unchecked (Design v3 §7: a narrowed check never
// quietly shrinks what submit requires).
func treeEvidence(r store.Reader, primary model.Run, runs []model.Run) (Evidence, error) {
	plan, err := r.Plan(primary.Plan)
	if err != nil {
		return Evidence{}, err
	}
	evidence, err := runEvidence(r, primary, plan)
	if err != nil {
		return Evidence{}, err
	}
	for _, target := range plan.Omitted {
		te := TargetEvidence{Target: target}
		for range plan.Environments {
			te.Outcomes = append(te.Outcomes, model.TargetResult{Target: target.ID, Outcome: model.OutcomeNotRun})
		}
		evidence.Targets = append(evidence.Targets, te)
	}
	for _, run := range runs {
		if run.ID == primary.ID || !evidence.missing() {
			continue
		}
		earlierPlan, err := r.Plan(run.Plan)
		if err != nil {
			return Evidence{}, err
		}
		earlier, err := runEvidence(r, run, earlierPlan)
		if err != nil {
			return Evidence{}, err
		}
		if evidence.fill(earlier) {
			evidence.Earlier = append(evidence.Earlier, run)
		}
	}
	evidence.settle()
	return evidence, nil
}

// missing reports whether any target has no result in an environment it
// is required in.
func (e Evidence) missing() bool {
	for _, target := range e.Targets {
		for i, result := range target.Outcomes {
			if result.Outcome == model.OutcomeNotRun && !Excluded(e.Plan, target.Target, e.Plan.Environments[i].Platform) {
				return true
			}
		}
	}
	return false
}

// fill takes an earlier check's results for what this evidence lacks, in
// the same environment, and reports whether it took any.
func (e *Evidence) fill(earlier Evidence) bool {
	took := false
	for t := range e.Targets {
		target := &e.Targets[t]
		k := slices.IndexFunc(earlier.Targets, func(other TargetEvidence) bool { return other.Target.ID == target.Target.ID })
		if k < 0 {
			continue
		}
		for i, result := range target.Outcomes {
			environment := e.Plan.Environments[i]
			if result.Outcome != model.OutcomeNotRun || Excluded(e.Plan, target.Target, environment.Platform) {
				continue
			}
			j := slices.Index(earlier.Plan.Environments, environment)
			if j < 0 || Excluded(earlier.Plan, earlier.Targets[k].Target, environment.Platform) {
				continue
			}
			if found := earlier.Targets[k].Outcomes[j]; found.Outcome != model.OutcomeNotRun {
				target.Outcomes[i] = found
				took = true
			}
		}
	}
	return took
}

// settle works out each target's verdict from its outcomes: passed where
// it passed in every environment it is required in, and unchecked where
// one has no result.
func (e *Evidence) settle() {
	for t := range e.Targets {
		target := &e.Targets[t]
		target.Passed, target.Unchecked = true, false
		for i, result := range target.Outcomes {
			if Excluded(e.Plan, target.Target, e.Plan.Environments[i].Platform) {
				continue
			}
			target.Passed = target.Passed && result.Outcome == model.OutcomePassed
			target.Unchecked = target.Unchecked || result.Outcome == model.OutcomeNotRun
		}
	}
}

// publicationProblems applies the publication rule to evidence: every
// changed target checked and every substantive one passed, and every
// other failure accepted.
func publicationProblems(evidence Evidence, accepted []string) []string {
	var problems []string
	for _, target := range evidence.Failed() {
		name := target.Target.Target.Name
		switch {
		case target.Unchecked && target.Target.Role != model.Also:
			problems = append(problems, fmt.Sprintf("%s is changed, and no check of these files built it everywhere it's required; dockhand check builds it, or share the branch as a draft (--draft)", name))
		case !Acceptable(target.Target):
			problems = append(problems, fmt.Sprintf("%s did not pass in %s; fix it, or share it as a draft (--draft)", name, evidence.Run.Name()))
		case !slices.Contains(accepted, name):
			problems = append(problems, fmt.Sprintf("%s (%s) did not pass in %s; acknowledge it with --accept %s if its failure is not this branch's doing", name, kindWords(target.Target), evidence.Run.Name(), name))
		}
	}
	return problems
}

func kindWords(target model.PlanTarget) string {
	if target.Role == model.Also {
		return "an extra from --also"
	}
	if target.Kind == model.RevisionOnly {
		return "revision bump only"
	}
	return "changed"
}

// Excluded reports whether the plan leaves a target out on a platform,
// where it is not built and not required to pass.
func Excluded(plan model.Plan, target model.PlanTarget, platform model.Platform) bool {
	return slices.ContainsFunc(plan.Exclusions, func(x model.Exclusion) bool {
		return x.Target.Name == target.Target.Name && x.Platform == platform
	})
}
