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
// check built again at the master that check started from, in the same
// environments, to see what they do without the branch.
type Baseline struct {
	// Of is the branch's check it looks into, and OfPlan its plan.
	Of     model.Run
	OfPlan model.Plan
	// Revision is the checked revision's base, as a revision of the branch.
	Revision model.Revision
	Plan     model.Plan
	// New are the ports left out because the base has no Portfile for
	// them: the branch adds them, so there is nothing to compare.
	New []string
	// Skipped are failed ports left out without --only because they failed
	// only before building, at lint, fetch, or checksum: those come from
	// the branch's own Portfile and distfiles, which a build at the base
	// can't speak to.
	Skipped []string
}

// PlanBaseline plans a baseline of the branch's newest finished check: of
// the ports named, or else of the ports that failed in it at install or
// test (BaselineWorthy). It is planned the way a check
// is, from the base that check started from, not the branch's base since
// a rebase: the base's own Portfiles are evaluated in each environment, so
// its exclusions, dependencies, and needs are the base's, and a port that
// needs Xcode is unmet where there is none, not sent there.
func (e *Engine) PlanBaseline(ctx context.Context, branch model.Branch, ports []string) (Baseline, error) {
	var baseline Baseline
	var checked model.Revision
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
		if baseline.OfPlan, err = r.Plan(baseline.Of.Plan); err != nil {
			return err
		}
		checked, err = r.Revision(baseline.Of.Revision)
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
		ports, baseline.Skipped = BaselineWorthy(evidence)
		if len(ports) == 0 {
			if len(baseline.Skipped) > 0 {
				return baseline, fmt.Errorf("%s failed only before building, at lint, fetch, or checksum, which master can't speak to; --only builds them there anyway", strings.Join(baseline.Skipped, ", "))
			}
			return baseline, fmt.Errorf("nothing failed in %s, so there is nothing to compare; name ports with --only", baseline.Of.Name())
		}
	}
	base := checked.Source.Base
	trees, err := e.Repo.CommitTrees(ctx, []string{string(base)})
	if err != nil {
		return baseline, err
	}
	baseTree := trees[string(base)]
	var also []string
	directories := map[string]string{}
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
		also = append(also, name)
		directories[name] = target.Directory
	}
	if len(also) == 0 {
		return baseline, fmt.Errorf("%s: master %s has none of them, so there is nothing to compare", strings.Join(baseline.New, ", "), short(base))
	}
	if baseline.Revision, err = e.baseRevision(ctx, branch, base, baseTree); err != nil {
		return baseline, err
	}
	baseline.Plan, err = e.PlanCheck(ctx, PlanRequest{Revision: baseline.Revision, Environments: baseline.OfPlan.Environments, Also: also, Tests: baseline.OfPlan.Tests, directories: directories, alone: true})
	if err != nil {
		return baseline, err
	}
	if len(baseline.Plan.Unresolved) > 0 {
		var reasons []string
		for _, unresolved := range baseline.Plan.Unresolved {
			reasons = append(reasons, unresolved.Target.Name+": "+unresolved.Reason)
		}
		return baseline, fmt.Errorf("master %s can't be planned: %s", short(base), strings.Join(reasons, "; "))
	}
	if len(baseline.Plan.Targets) == 0 {
		return baseline, fmt.Errorf("master %s builds none of %s anywhere this check built", short(base), strings.Join(also, ", "))
	}
	return baseline, nil
}

// BaselineWorthy sorts a check's failed ports into those a baseline can
// say something about, that failed at install or test somewhere, and those
// it can't, that failed only before building, at lint, fetch, or checksum,
// which come from the branch's own Portfile and distfiles. A port blocked,
// unmet, or not evaluated is neither.
func BaselineWorthy(evidence Evidence) (worthy, skipped []string) {
	for _, target := range evidence.Failed() {
		built, early := false, false
		for _, result := range target.Outcomes {
			if result.Outcome != model.OutcomeFailed {
				continue
			}
			switch result.Phase {
			case model.PhaseInstall, model.PhaseTest:
				built = true
			case model.PhaseLint, model.PhaseFetch, model.PhaseChecksum:
				early = true
			}
		}
		switch {
		case built:
			worthy = append(worthy, string(target.Target.ID))
		case early:
			skipped = append(skipped, string(target.Target.ID))
		}
	}
	return worthy, skipped
}

// baseRevision is a base, as a revision of the branch, recorded once.
func (e *Engine) baseRevision(ctx context.Context, branch model.Branch, base model.ObjectID, tree string) (model.Revision, error) {
	revision := model.Revision{Branch: branch.ID, Kind: model.RevisionCommit, Head: base, CreatedAt: e.now(),
		Source: model.Source{Commit: base, Tree: model.ObjectID(tree), Base: base}}
	err := e.Store.Update(ctx, e.Repository, func(tx store.Tx) error {
		existing, err := tx.Revisions(branch.ID)
		if err != nil {
			return err
		}
		for _, earlier := range existing {
			if earlier.Kind == model.RevisionCommit && earlier.Source.Commit == base {
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
