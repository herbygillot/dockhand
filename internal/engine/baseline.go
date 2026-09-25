package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// Baseline is a planned baseline (Design v3 §6.8): ports of a branch's
// check built again at the master the branch starts from, in the same
// environments, to see what they do without the branch.
type Baseline struct {
	// Of is the branch's check it looks into, and OfPlan its plan.
	Of     model.Run
	OfPlan model.Plan
	// Revision is the branch's base, as a revision of the branch.
	Revision model.Revision
	Plan     model.Plan
	// New are the ports left out because the base has no Portfile for
	// them: the branch adds them, so there is nothing to compare.
	New []string
}

// PlanBaseline plans a baseline of the branch's newest finished check: of
// the ports named, or else of every port that failed in it.
func (e *Engine) PlanBaseline(ctx context.Context, branch model.Branch, ports []string) (Baseline, error) {
	var baseline Baseline
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		runs, err := r.Runs(store.RunFilter{Branch: branch.ID, States: []model.RunState{model.RunPassed, model.RunFailed, model.RunAttention}})
		if err != nil {
			return err
		}
		i := slices.IndexFunc(runs, func(run model.Run) bool { return run.BaselineOf == "" })
		if i < 0 {
			return fmt.Errorf("%s has no finished check to compare with; run dockhand check first", branch.ShortName())
		}
		baseline.Of = runs[i]
		baseline.OfPlan, err = r.Plan(baseline.Of.Plan)
		return err
	})
	if err != nil {
		return baseline, err
	}
	if len(ports) == 0 {
		evidence, err := e.RunEvidence(ctx, baseline.Of.ID)
		if err != nil {
			return baseline, err
		}
		for _, target := range evidence.Failed() {
			ports = append(ports, string(target.Target.ID))
		}
		if len(ports) == 0 {
			return baseline, fmt.Errorf("nothing failed in %s, so there is nothing to compare; name ports with --only", baseline.Of.Name())
		}
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(branch.Base)})
	if err != nil {
		return baseline, err
	}
	baseTree := trees[string(branch.Base)]
	var targets []model.PlanTarget
	for _, name := range ports {
		target, ok := baseline.OfPlan.Target(model.TargetID(name))
		if !ok {
			return baseline, fmt.Errorf("--only %s: %s did not build it", name, baseline.Of.Name())
		}
		state, _, err := e.Repo.File(ctx, baseTree, target.Target.Portfile)
		if err != nil {
			return baseline, err
		}
		if !state.Exists {
			baseline.New = append(baseline.New, name)
			continue
		}
		target.Kind, target.Role = model.Unchanged, model.Also
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		return baseline, fmt.Errorf("%s: master %s has none of them, so there is nothing to compare", strings.Join(baseline.New, ", "), short(branch.Base))
	}
	for i := range targets {
		targets[i].DependsOn = slices.DeleteFunc(slices.Clone(targets[i].DependsOn), func(id model.TargetID) bool {
			return !slices.ContainsFunc(targets, func(t model.PlanTarget) bool { return t.ID == id })
		})
	}
	if baseline.Revision, err = e.baseRevision(ctx, branch, baseTree); err != nil {
		return baseline, err
	}
	var also []string
	for _, target := range targets {
		also = append(also, string(target.ID))
	}
	// Each environment's dependencies carry over, among the targets kept.
	var dependencies []map[model.TargetID][]model.TargetID
	for _, needs := range baseline.OfPlan.Dependencies {
		kept := map[model.TargetID][]model.TargetID{}
		for id, deps := range needs {
			if !slices.Contains(also, string(id)) {
				continue
			}
			if deps = slices.DeleteFunc(slices.Clone(deps), func(d model.TargetID) bool { return !slices.Contains(also, string(d)) }); len(deps) > 0 {
				kept[id] = deps
			}
		}
		dependencies = append(dependencies, kept)
	}
	baseline.Plan = model.Plan{ID: model.PlanID(store.NewID("plan")), Revision: baseline.Revision.ID, Environments: baseline.OfPlan.Environments,
		Targets: targets, Dependencies: dependencies, Also: also, Tests: baseline.OfPlan.Tests, CreatedAt: e.now()}
	return baseline, baseline.Plan.Validate()
}

// baseRevision is the branch's base as a revision of the branch, recorded
// once.
func (e *Engine) baseRevision(ctx context.Context, branch model.Branch, tree string) (model.Revision, error) {
	revision := model.Revision{Branch: branch.ID, Kind: model.RevisionCommit, Head: branch.Base, CreatedAt: e.now(),
		Source: model.Source{Commit: branch.Base, Tree: model.ObjectID(tree), Base: branch.Base}}
	err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		existing, err := tx.Revisions(branch.ID)
		if err != nil {
			return err
		}
		for _, earlier := range existing {
			if earlier.Kind == model.RevisionCommit && earlier.Source.Commit == branch.Base {
				revision = earlier
				return nil
			}
		}
		revision.ID = model.RevisionID(store.NewID("rev"))
		return tx.AddRevision(revision)
	})
	return revision, err
}

// EnqueueBaseline queues a planned baseline as a run marked as one.
func (e *Engine) EnqueueBaseline(ctx context.Context, branch model.Branch, baseline Baseline, origin model.Origin) (model.Run, error) {
	if baseline.Of.ID == "" {
		return model.Run{}, errors.New("engine: a baseline names the check it looks into")
	}
	return e.enqueue(ctx, branch, baseline.Plan, origin, baseline.Of.ID)
}
