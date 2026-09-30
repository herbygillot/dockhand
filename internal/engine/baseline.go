package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/planning"
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
// test (BaselineWorthy), each rebuilt only where it failed (rebuildWhere).
// It is planned the way a check is, from the base that check started
// from, not the branch's base since a rebase: the base's own Portfiles are
// evaluated in each environment, so its exclusions, dependencies, and
// needs are the base's, and a port that needs Xcode is unmet where there
// is none, not sent there.
func (e *Engine) PlanBaseline(ctx context.Context, branch model.Branch, ports []string) (Baseline, error) {
	return e.planBaseline(ctx, branch, ports, true)
}

// PreviewBaseline plans a baseline as PlanBaseline does, and records
// nothing, not even the base as a revision of the branch: what check
// --baseline --plan shows.
func (e *Engine) PreviewBaseline(ctx context.Context, branch model.Branch, ports []string) (Baseline, error) {
	return e.planBaseline(ctx, branch, ports, false)
}

func (e *Engine) planBaseline(ctx context.Context, branch model.Branch, ports []string, record bool) (Baseline, error) {
	var baseline Baseline
	var checked model.Revision
	var found bool
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if baseline.Of, found, err = latestCheck(r, branch.ID); err != nil || !found {
			return err
		}
		if baseline.OfPlan, err = r.Plan(baseline.Of.Plan); err != nil {
			return err
		}
		checked, err = r.Revision(baseline.Of.Revision)
		return err
	})
	if err != nil {
		return baseline, err
	}
	if !found {
		return baseline, fmt.Errorf("%s has no finished check to compare with; run dockhand check first", branch.ShortName())
	}
	evidence, err := e.RunEvidence(ctx, baseline.Of.ID)
	if err != nil {
		return baseline, err
	}
	if len(ports) == 0 {
		ports, baseline.Skipped = BaselineWorthy(evidence)
		if len(ports) == 0 {
			if len(baseline.Skipped) > 0 {
				return baseline, fmt.Errorf("%s failed only before building, at lint, fetch, or checksum, which master can't speak to; --only builds them there anyway", strings.Join(baseline.Skipped, ", "))
			}
			return baseline, fmt.Errorf("nothing failed in %s, so there is nothing to compare; name ports with --only", baseline.Of.Name())
		}
	}
	base := checked.Source.Base
	targets, added, baseTree, err := e.atBase(ctx, base, evidence, ports)
	if err != nil {
		return baseline, err
	}
	baseline.New = added
	var also []string
	directories := map[string]string{}
	where := map[model.TargetID]planning.Limited{}
	for _, target := range targets {
		also = append(also, string(target.ID))
		directories[string(target.ID)] = target.Directory
		where[target.ID] = rebuildWhere(evidence, target.ID)
	}
	if len(also) == 0 {
		return baseline, fmt.Errorf("%s: master %s has none of them, so there is nothing to compare", strings.Join(baseline.New, ", "), short(base))
	}
	if baseline.Revision, err = e.baseRevision(ctx, branch, base, baseTree, record); err != nil {
		return baseline, err
	}
	baseline.Plan, err = e.PlanCheck(ctx, PlanRequest{Revision: baseline.Revision, Environments: baseline.OfPlan.Environments, Also: also, Tests: baseline.OfPlan.Tests,
		directories: directories, alone: true, where: where})
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

// BaselineCandidates are what check --baseline would build after a failed
// check, for the check to point to it: the ports that failed at install or
// test (BaselineWorthy) that master has, and the base the check started
// from, where it would build them. There are none unless the check failed
// and is its branch's newest finished one, the one --baseline looks into.
func (e *Engine) BaselineCandidates(ctx context.Context, run model.Run) ([]string, model.ObjectID, error) {
	if run.State != model.RunFailed || run.BaselineOf != "" {
		return nil, "", nil
	}
	var latest model.Run
	var checked model.Revision
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if latest, _, err = latestCheck(r, run.Branch); err != nil || latest.ID != run.ID {
			return err
		}
		checked, err = r.Revision(run.Revision)
		return err
	})
	if err != nil || latest.ID != run.ID {
		return nil, "", err
	}
	evidence, err := e.RunEvidence(ctx, run.ID)
	if err != nil {
		return nil, "", err
	}
	worthy, _ := BaselineWorthy(evidence)
	if len(worthy) == 0 {
		return nil, "", nil
	}
	targets, _, _, err := e.atBase(ctx, checked.Source.Base, evidence, worthy)
	if err != nil || len(targets) == 0 {
		return nil, "", err
	}
	var ports []string
	for _, target := range targets {
		ports = append(ports, string(target.ID))
	}
	return ports, checked.Source.Base, nil
}

// rebuildWhere is where a baseline rebuilds a port: the environments where
// the check failed it at install or test, or its tests failed, or, for one
// named that failed nowhere there, every environment the check built it
// in.
func rebuildWhere(evidence Evidence, id model.TargetID) planning.Limited {
	i := slices.IndexFunc(evidence.Targets, func(target TargetEvidence) bool { return target.Target.ID == id })
	var failed, built []model.Environment
	for n, result := range evidence.Targets[i].Outcomes {
		environment := evidence.Plan.Environments[n]
		if planned, ok := evidence.Plan.In(environment); ok && planned.Builds(id) {
			built = append(built, environment)
		}
		if result.Outcome == model.OutcomeFailed && (result.Phase == model.PhaseInstall || result.Phase == model.PhaseTest) || result.Outcome == model.OutcomePassed && result.Tests.Failed() {
			failed = append(failed, environment)
		}
	}
	if len(failed) > 0 {
		return planning.Limited{Environments: failed, Elsewhere: evidence.Run.Name() + " didn't fail it there"}
	}
	return planning.Limited{Environments: built, Elsewhere: evidence.Run.Name() + " didn't build it there"}
}

// latestCheck is a branch's newest finished check, not a baseline: the
// one a baseline looks into.
func latestCheck(r store.Reader, branch model.BranchID) (model.Run, bool, error) {
	runs, err := r.Runs(store.RunFilter{Branch: branch, States: []model.RunState{model.RunPassed, model.RunFailed, model.RunAttention}})
	if err != nil {
		return model.Run{}, false, err
	}
	i := slices.IndexFunc(runs, func(run model.Run) bool { return run.BaselineOf == "" })
	if i < 0 {
		return model.Run{}, false, nil
	}
	return runs[i], true, nil
}

// atBase sorts ports a check built into those whose Portfile master has
// at base, and those the branch adds, with base's tree.
func (e *Engine) atBase(ctx context.Context, base model.ObjectID, evidence Evidence, names []string) (have []model.PlanTarget, added []string, tree string, err error) {
	trees, err := e.Repo.CommitTrees(ctx, []string{string(base)})
	if err != nil {
		return nil, nil, "", err
	}
	tree = trees[string(base)]
	for _, name := range names {
		i := slices.IndexFunc(evidence.Targets, func(target TargetEvidence) bool { return target.Target.ID == model.TargetID(name) })
		if i < 0 {
			return nil, nil, "", fmt.Errorf("--only %s: %s did not build it", name, evidence.Run.Name())
		}
		target := evidence.Targets[i].Target
		state, _, err := e.Repo.File(ctx, tree, target.Target.Portfile)
		if err != nil {
			return nil, nil, "", err
		}
		if !state.Exists {
			added = append(added, name)
			continue
		}
		have = append(have, target)
	}
	return have, added, tree, nil
}

// BaselineWorthy sorts a check's ports into those a baseline can say
// something about, that failed at install or test somewhere, or whose
// tests failed where the policy only reports them, as uvw's did in
// check-38, and those it can't, that failed only before building, at
// lint, fetch, or checksum, which come from the branch's own Portfile and
// distfiles. A port blocked, unmet, or not evaluated is neither.
func BaselineWorthy(evidence Evidence) (worthy, skipped []string) {
	for _, target := range evidence.Targets {
		built, early := false, false
		for _, result := range target.Outcomes {
			if result.Outcome == model.OutcomePassed && result.Tests.Failed() {
				built = true
			}
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

// baseRevision is a base, as a revision of the branch, recorded once; not
// recorded at all for a preview.
func (e *Engine) baseRevision(ctx context.Context, branch model.Branch, base model.ObjectID, tree string, record bool) (model.Revision, error) {
	revision := model.Revision{Branch: branch.ID, Kind: model.RevisionCommit, Head: base, CreatedAt: e.now(),
		Source: model.Source{Commit: base, Tree: model.ObjectID(tree), Base: base}}
	if !record {
		// The base's revision where one is recorded; else one with an ID
		// of its own, as the plan made of it has, though neither is
		// recorded, as a check's plan captures.
		err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
			existing, err := r.Revisions(branch.ID)
			if i := slices.IndexFunc(existing, func(earlier model.Revision) bool {
				return earlier.Kind == model.RevisionCommit && earlier.Source.Commit == base
			}); err == nil && i >= 0 {
				revision = existing[i]
				return nil
			}
			revision.ID = model.RevisionID(store.NewID("rev"))
			return err
		})
		return revision, err
	}
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
