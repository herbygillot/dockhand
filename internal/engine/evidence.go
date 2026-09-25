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
}

// Evidence is what the newest finished check of a tree established.
type Evidence struct {
	Run     model.Run
	Plan    model.Plan
	Targets []TargetEvidence
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

// EvidenceFor finds the newest finished check of a branch whose revision
// has the given tree, and what it established for each target. A result
// holds for its tree, whichever snapshot or commit was checked, because a
// check builds files, not history. It reports false when no finished
// check covers the tree.
func (e *Engine) EvidenceFor(ctx context.Context, branch model.BranchID, tree model.ObjectID) (Evidence, bool, error) {
	var evidence Evidence
	found := false
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		revisions, err := r.Revisions(branch)
		if err != nil {
			return err
		}
		matching := map[model.RevisionID]bool{}
		for _, revision := range revisions {
			if revision.Source.Tree == tree {
				matching[revision.ID] = true
			}
		}
		if len(matching) == 0 {
			return nil
		}
		runs, err := r.Runs(store.RunFilter{Branch: branch, States: []model.RunState{model.RunPassed, model.RunFailed, model.RunAttention}})
		if err != nil {
			return err
		}
		for _, run := range runs {
			if !matching[run.Revision] {
				continue
			}
			plan, err := r.Plan(run.Plan)
			if err != nil {
				return err
			}
			if evidence, err = runEvidence(r, run, plan); err != nil {
				return err
			}
			found = true
			return nil
		}
		return nil
	})
	return evidence, found, err
}

// publicationProblems applies the publication rule to evidence: every
// substantive target passed, and every other failure accepted.
func publicationProblems(evidence Evidence, accepted []string) []string {
	var problems []string
	for _, target := range evidence.Failed() {
		name := target.Target.Target.Name
		switch {
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
