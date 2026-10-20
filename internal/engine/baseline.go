package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/planning"
	"github.com/herbygillot/dockhand/internal/prose"
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
	// NewTests are failed ports left out without --only because only their
	// tests failed, and master declares none (testsNewHere).
	NewTests []string
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
		if ports, baseline.NewTests, err = e.testsNewHere(ctx, checked, evidence, ports); err != nil {
			return baseline, err
		}
		if len(ports) == 0 && len(baseline.NewTests) > 0 {
			return baseline, errors.New(NewTestsWords(baseline.NewTests))
		}
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
		return baseline, fmt.Errorf("%s: master %s has none of them, so there is nothing to compare", strings.Join(baseline.New, ", "), base.Short())
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
		return baseline, fmt.Errorf("master %s can't be planned: %s", base.Short(), strings.Join(reasons, "; "))
	}
	if len(baseline.Plan.Targets) == 0 {
		return baseline, fmt.Errorf("master %s builds none of %s anywhere this check built", base.Short(), strings.Join(also, ", "))
	}
	return baseline, nil
}

// BaselineOffer is what check --baseline would build after a failed check,
// for the check to point to it: Ports, the ports that failed at install or
// test (BaselineWorthy) that master has, at Base, the base the check
// started from; and NewTests, those left out since only their tests
// failed and master declares none, which the check says instead.
type BaselineOffer struct {
	Ports    []string
	Base     model.ObjectID
	NewTests []string
}

// BaselineCandidates are the baseline a failed check offers. There is none
// unless the check failed and is its branch's newest finished one, the one
// --baseline looks into.
func (e *Engine) BaselineCandidates(ctx context.Context, run model.Run) (BaselineOffer, error) {
	if run.State != model.RunFailed || run.BaselineOf != "" {
		return BaselineOffer{}, nil
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
		return BaselineOffer{}, err
	}
	evidence, err := e.RunEvidence(ctx, run.ID)
	if err != nil {
		return BaselineOffer{}, err
	}
	worthy, _ := BaselineWorthy(evidence)
	var offer BaselineOffer
	if worthy, offer.NewTests, err = e.testsNewHere(ctx, checked, evidence, worthy); err != nil || len(worthy) == 0 {
		return offer, err
	}
	if worthy, err = e.readsChanged(ctx, checked, evidence, worthy); err != nil || len(worthy) == 0 {
		return offer, err
	}
	targets, _, _, err := e.atBase(ctx, checked.Source.Base, evidence, worthy)
	if err != nil || len(targets) == 0 {
		return offer, err
	}
	for _, target := range targets {
		offer.Ports = append(offer.Ports, string(target.ID))
	}
	offer.Base = checked.Source.Base
	return offer, nil
}

// readsChanged are the ports, of those a baseline would rebuild, whose
// build reads something the branch changes: its own directory,
// _resources, or a port of its dependency closure, by the revision's port
// index. One that reads only what master has is master's build already,
// so a baseline can tell nothing: gnome-voice's check-484, of a branch
// that builds it --also unchanged, offered one (field testing,
// 2026-10-20). An --also dependent of a port the branch changes builds
// against the change, and keeps its offer. Where the closure can't be
// read, a port is kept.
func (e *Engine) readsChanged(ctx context.Context, checked model.Revision, evidence Evidence, ports []string) ([]string, error) {
	trees, err := e.Repo.CommitTrees(ctx, []string{string(checked.Source.Base)})
	if err != nil {
		return nil, err
	}
	changed, err := e.Repo.ChangedPaths(ctx, trees[string(checked.Source.Base)], string(checked.Source.Tree))
	if err != nil {
		return nil, err
	}
	var directories []string
	for _, path := range changed {
		if path == macports.ResourcesDirectory || strings.HasPrefix(path, macports.ResourcesDirectory+"/") {
			return ports, nil
		}
		if directory, ok := macports.PortDirectoryOf(path); ok && !slices.Contains(directories, directory) {
			directories = append(directories, directory)
		}
	}
	// The ports the changed directories define, in lower case, as the
	// index's graph names them.
	names := map[string]bool{}
	var graph map[string][]string
	if len(directories) > 0 {
		defined := e.portsDefined(ctx, checked.Source, directories)
		reader, err := e.portReader()
		graphs, ok := reader.(dependencyGraphReader)
		if defined == nil || err != nil || !ok {
			return ports, nil
		}
		if graph, err = graphs.DependencyGraph(ctx, checked.Source); err != nil {
			return ports, nil
		}
		for _, all := range defined {
			for _, name := range all {
				names[strings.ToLower(name)] = true
			}
		}
	}
	var kept []string
	for _, port := range ports {
		i := slices.IndexFunc(evidence.Targets, func(target TargetEvidence) bool { return string(target.Target.ID) == port })
		if i < 0 {
			kept = append(kept, port)
			continue
		}
		target := evidence.Targets[i].Target
		if slices.Contains(directories, target.Directory) || closureReaches(graph, target.Target.Name, target.Target.Subport, names) {
			kept = append(kept, port)
		}
	}
	return kept, nil
}

// closureReaches reports whether a port's dependency closure in graph
// holds one of names, all in lower case.
func closureReaches(graph map[string][]string, name, subport string, names map[string]bool) bool {
	if subport != "" {
		name = subport
	}
	seen := map[string]bool{}
	queue := []string{strings.ToLower(name)}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, dependency := range graph[next] {
			if names[dependency] {
				return true
			}
			if !seen[dependency] {
				seen[dependency] = true
				queue = append(queue, dependency)
			}
		}
	}
	return false
}

// testsNewHere sets apart, of the ports a baseline would rebuild, those
// whose only failures are their tests' where the branch declares tests
// master doesn't, by the change record's dockhand.test_run: a baseline
// can't say whether master fails tests it doesn't run, and exiv2's check
// offered one (the exiv2 field notes, 2026-10-19). A port with no record
// to say is kept.
func (e *Engine) testsNewHere(ctx context.Context, checked model.Revision, evidence Evidence, ports []string) (kept, newTests []string, err error) {
	records, err := e.revisionChanges(ctx, checked.Branch, checked.Source.Base, checked.Source.Tree, false)
	if err != nil {
		return nil, nil, err
	}
	for _, port := range ports {
		i := slices.IndexFunc(evidence.Targets, func(target TargetEvidence) bool { return string(target.Target.ID) == port })
		if i >= 0 && onlyTestsFailed(evidence.Targets[i]) && testsDeclaredHere(records, evidence.Targets[i].Target) {
			newTests = append(newTests, port)
			continue
		}
		kept = append(kept, port)
	}
	return kept, newTests, nil
}

// onlyTestsFailed reports a target whose every failure while it built
// (failedAt) is its tests': the install itself passed wherever it ran.
func onlyTestsFailed(target TargetEvidence) bool {
	failed := false
	for _, outcome := range target.Outcomes {
		result := outcome.TargetResult
		building, _ := failedAt(result)
		if !building {
			continue
		}
		if result.Outcome == model.OutcomeFailed && result.Phase != model.PhaseTest {
			return false
		}
		failed = true
	}
	return failed
}

// testsDeclaredHere reports a target whose change record says its tests
// are new: dockhand.test_run moved from false to true against the base.
func testsDeclaredHere(records map[string]model.ChangeRecord, target model.PlanTarget) bool {
	record, ok := records[target.Directory]
	if !ok {
		return false
	}
	for _, port := range record.Ports {
		if port.Port != target.Target.Name {
			continue
		}
		for _, field := range port.Fields {
			if field.Field != "dockhand.test_run" {
				continue
			}
			was, _ := macports.ParseBool(field.From)
			is, _ := macports.ParseBool(field.To)
			return !was && is
		}
	}
	return false
}

// NewTestsWords say why a baseline isn't offered for ports whose tests are
// new on the branch.
func NewTestsWords(ports []string) string {
	tests := "tests are"
	if len(ports) > 1 {
		tests = "tests are each"
	}
	return fmt.Sprintf("%s's %s new on this branch, and master declares none, so a baseline has nothing to compare", prose.And(ports), tests)
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
		if building, _ := failedAt(result.TargetResult); building {
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
	runs, err := r.Runs(store.RunFilter{Branch: branch, States: []model.RunState{model.RunPassed, model.RunFailed, model.RunAttention}, NoBaselines: true, Limit: 1})
	if err != nil || len(runs) == 0 {
		return model.Run{}, false, err
	}
	return runs[0], true, nil
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

// failedAt is the one rule for what a baseline can speak to in a result
// (the helper-ownership review's table): building is a failure while the
// port built, at install or test, or tests that failed where the policy
// only reports them; before is a failure before it built, at lint,
// fetch, or checksum, which comes from the branch's own Portfile and
// distfiles, and a baseline can't answer. A result blocked, unmet, or not
// run is neither.
func failedAt(result model.TargetResult) (building, before bool) {
	switch {
	case result.Outcome == model.OutcomePassed:
		return result.Tests.Failed(), false
	case result.Outcome != model.OutcomeFailed:
		return false, false
	}
	switch result.Phase {
	case model.PhaseInstall, model.PhaseTest:
		return true, false
	case model.PhaseLint, model.PhaseFetch, model.PhaseChecksum:
		return false, true
	}
	return false, false
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
			building, before := failedAt(result.TargetResult)
			built, early = built || building, early || before
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

// BaselineRow is one port's result at the base beside the branch's, in one
// environment: Excluded where the base doesn't build it there, and why.
type BaselineRow struct {
	Target      model.TargetID
	Environment model.Environment
	Base        model.TargetResult
	Branch      model.TargetResult
	Excluded    string
}

// BaselineComparison sets each port's result at the base beside the
// branch's, in each environment where the baseline built it, and where the
// check failed it but master doesn't build it. A baseline rebuilds a port
// only where it failed, so elsewhere there is nothing to set beside (the
// command package review's row 6, moved here from check's printing).
func BaselineComparison(base, branch Evidence) []BaselineRow {
	var rows []BaselineRow
	for _, target := range base.Targets {
		i := slices.IndexFunc(branch.Targets, func(t TargetEvidence) bool { return t.Target.ID == target.Target.ID })
		for n, result := range target.Outcomes {
			environment := base.Plan.Environments[n]
			theirs := model.TargetResult{Outcome: model.OutcomeNotRun}
			if j := slices.Index(branch.Plan.Environments, environment); i >= 0 && j >= 0 {
				theirs = branch.Targets[i].Outcomes[j].TargetResult
			}
			planned, _ := base.Plan.In(environment)
			if !planned.Builds(target.Target.ID) && theirs.Outcome != model.OutcomeFailed {
				continue
			}
			row := BaselineRow{Target: target.Target.ID, Environment: environment, Base: result.TargetResult, Branch: theirs}
			if exclusion, excluded := base.Plan.ExclusionIn(environment, target.Target.ID); excluded {
				row.Excluded = exclusion.Reason
			}
			rows = append(rows, row)
		}
	}
	return rows
}
