package engine

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/herbygillot/dockhand/internal/branchstate"
	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/checks"
	"github.com/herbygillot/dockhand/internal/evidence"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// What recorded checks establish is the evidence package's to say (the
// architecture review's finding 4); the engine loads the records it
// reads, asks providers what each environment is now, and words what it
// says. Its types keep their names here, for the commands that read them.
type (
	Evidence       = evidence.Evidence
	TargetEvidence = evidence.TargetEvidence
	Cell           = evidence.Cell
	CellKind       = evidence.CellKind
	VariantBuild   = evidence.VariantBuild
)

const (
	CellRecorded = evidence.CellRecorded
	CellExcluded = evidence.CellExcluded
	CellUnmet    = evidence.CellUnmet
	CellNotRun   = evidence.CellNotRun
	CellRemade   = evidence.CellRemade
	CellEntered  = evidence.CellEntered
)

// CarriedLines say, of the evidence's results reused over a change that
// builds nothing differently (model.Carried), what changed since each
// earlier check built them, a sentence for each check that reused and
// each it reused from, in the targets' order (checks.CarriedWords):
// "check-767 reused check-766's builds (commit 3f2a1c9): since then only
// omnidiff's maintainers changed, which builds nothing differently, and
// lint ran again on this Portfile". None where nothing was carried.
func CarriedLines(e Evidence) []string {
	type pair struct{ reusing, from string }
	var pairs []pair
	carried := map[pair][]model.Carried{}
	names := e.Checks()
	for _, target := range e.Targets {
		for _, cell := range target.Outcomes {
			if cell.Carried == nil {
				continue
			}
			key := pair{names[e.Executions[cell.Execution].Run], cell.Carried.Check}
			if key.reusing == "" {
				key.reusing = e.Run.Name()
			}
			if _, ok := carried[key]; !ok {
				pairs = append(pairs, key)
			}
			carried[key] = append(carried[key], *cell.Carried)
		}
	}
	var lines []string
	for _, key := range pairs {
		lines = append(lines, checks.CarriedWords(key.reusing, carried[key]))
	}
	return lines
}

// RemadeWords says what changed in the first environment where the
// target's result was recorded before it changed: its provider's words
// where it can say (buildenv.IdentityExplainer), as Tart says a new guest
// protocol, and otherwise that the environment was made again.
func RemadeWords(target TargetEvidence) string {
	for _, c := range target.Outcomes {
		if c.Kind != CellRemade {
			continue
		}
		if c.Change != "" {
			return "on " + checks.DescribeEnvironment(c.Environment) + ", " + c.Change
		}
		return checks.DescribeEnvironment(c.Environment) + " was made again, from another source or with other tools"
	}
	return ""
}

// EvidenceWords is how one target's result in one environment reads, on the
// terminal and in the pull request alike (TargetWords). A result's tests
// read under the policy of the check that built it: when that check is an
// earlier one whose policy differs from this evidence's own, it is named,
// "tests failed (advisory, check-3)", so a later check with --tests
// required never makes an earlier advisory result read as required.
func EvidenceWords(evidence Evidence, target TargetEvidence, environment int, accepted AcceptedFailure) string {
	result := target.Outcomes[environment]
	return targetWords(evidence.Plan, result, evidence.TestsReading(result.TargetResult), accepted)
}

// EvidenceFor finds what the finished checks of a branch's tree
// established (evidence.Judge). A result holds for its tree, whichever
// snapshot or commit was checked, because a check builds files, not
// history. It reports false when no finished check covers the tree.
func (e *Engine) EvidenceFor(ctx context.Context, branch model.BranchID, tree model.ObjectID) (Evidence, bool, error) {
	var runs []model.Run
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		runs, err = treeRuns(r, branch, tree)
		return err
	}); err != nil || len(runs) == 0 {
		return Evidence{}, false, err
	}
	judged, err := e.evidenceNow(ctx, runs[0], runs)
	return judged, err == nil, err
}

// identitiesNow asks each environment's provider what it is now
// (checks.Service.Identities).
func (e *Engine) identitiesNow(ctx context.Context, environments []model.Environment) evidence.Identities {
	return e.checks().Identities(ctx, environments)
}

// evidenceNow is what a tree's checks establish now (evidence.Judge): the
// plan they're judged against, then the environments' identities, read
// outside any transaction, then the checks' records.
func (e *Engine) evidenceNow(ctx context.Context, primary model.Run, runs []model.Run) (Evidence, error) {
	var own model.Plan
	var others []model.Plan
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if own, err = r.Plan(primary.Plan); err != nil {
			return err
		}
		for _, run := range runs {
			if run.ID == primary.ID {
				continue
			}
			earlier, err := r.Plan(run.Plan)
			if err != nil {
				return err
			}
			others = append(others, earlier)
		}
		return nil
	}); err != nil {
		return Evidence{}, err
	}
	// The ask is the newest check's that covers every changed port: one
	// narrowed with --only adds its results but sets none, so exiv2's
	// check --on tart:13,14 --only ufraw doesn't make 13 and 14 the
	// branch's requirement (D17).
	ask := evidence.AskOf(append([]model.Plan{own}, others...))
	required, problem := e.requiredFor(ctx, ask)
	plan := evidence.Plan(own, others, required)
	now := e.identitiesNow(ctx, plan.Environments)
	var judged Evidence
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		// The newest check's own results are asked what their builds
		// fetched too, where finished coverage asks it (change 1 of
		// docs/designs/source-comparison.md): loaded without what they
		// read, every Git result answered "not known", and no check of a
		// Git-fetched port stood for another request.
		own, err := checks.LoadCheck(r, primary, evidence.ReadsSources(plan))
		if err != nil {
			return err
		}
		var earlier []evidence.Check
		for _, run := range runs {
			if run.ID == primary.ID {
				continue
			}
			check, err := checks.LoadCheck(r, run, evidence.ReadsSources(plan))
			if err != nil {
				return err
			}
			earlier = append(earlier, check)
		}
		judged = evidence.Judge(own, plan, earlier, now, required)
		judged.Problem, judged.Ask = problem, ask
		// A passed result no later check can reuse says why, so a rebuild
		// isn't a silent hour (the architecture review's L3a).
		for t := range judged.Targets {
			for i, c := range judged.Targets[t].Outcomes {
				if c.Outcome != model.OutcomePassed || c.Inputs == "" {
					continue
				}
				inputs, err := r.Inputs(c.Inputs)
				if err != nil {
					return err
				}
				judged.Targets[t].Outcomes[i].NotReusable = inputs.NotReusable
			}
		}
		return nil
	})
	// What changed where a result no longer stands is its provider's to
	// say, outside the transaction, as identities are read.
	for t := range judged.Targets {
		for i, c := range judged.Targets[t].Outcomes {
			if c.Kind != CellRemade || c.Recorded == "" {
				continue
			}
			if explainer, ok := e.Providers[c.Environment.Provider].(buildenv.IdentityExplainer); ok {
				judged.Targets[t].Outcomes[i].Change = explainer.IdentityChange(c.Environment, c.Recorded, now[c.Environment])
			}
		}
	}
	return judged, err
}

// treeRuns lists a branch's finished checks of a tree, newest first. A
// baseline is evidence about another run, never the branch's own check.
func treeRuns(r store.Reader, branch model.BranchID, tree model.ObjectID) ([]model.Run, error) {
	return runsOfTree(r, branch, tree, model.Finished...)
}

// runsOfTree are a branch's checks of a tree in the given states, newest
// first, baselines aside.
func runsOfTree(r store.Reader, branch model.BranchID, tree model.ObjectID, states ...model.RunState) ([]model.Run, error) {
	return r.Runs(store.RunFilter{Branch: branch, Tree: tree, States: states, NoBaselines: true})
}

// requiredFor are the environments a branch's files need, judged by the
// ask, the newest check of them covering every changed port: what the
// person asked it to build on, with --on or --build, exactly, local
// included where Tart is set up; else its own and build.default's
// (requiredEnvironments), and build.default's alone where no check sets
// an ask. The newest ask replaces an earlier one, so an ask that was
// dropped doesn't hold until it passes (D17, the person's "Newest ask",
// 2026-10-29); a failure in any check of the files still does, while its
// environment stands (evidence.Judge).
func (e *Engine) requiredFor(ctx context.Context, ask *evidence.Ask) ([]model.Environment, string) {
	if ask != nil && ask.Asked {
		return slices.Clone(ask.Environments), ""
	}
	profile, problem := e.requiredEnvironments(ctx)
	var required []model.Environment
	if ask != nil {
		required = slices.Clone(ask.Environments)
	}
	for _, environment := range profile {
		if !slices.ContainsFunc(required, func(planned model.Environment) bool { return evidence.Covers(environment, planned) }) {
			required = append(required, environment)
		}
	}
	return required, problem
}

// requiredEnvironments are the environments build.default's profile builds
// in, which a branch's evidence requires where its newest check named
// none (D17); none where they can't be resolved, as where a provider isn't
// set up, with why, for the evidence to say, where it was dropped and the
// evidence required less without a word (the architecture review's L5).
//
// One resolution stands for requiredWait: resolving a profile lists Tart's
// images, half a minute on the M1 and two at most (tart.ErrListingTimedOut),
// and status judged each branch's evidence with a listing of its own.
func (e *Engine) requiredEnvironments(ctx context.Context) ([]model.Environment, string) {
	r := &e.required
	r.mu.Lock()
	defer r.mu.Unlock()
	profile := e.defaultProfile()
	if !r.at.IsZero() && time.Since(r.at) < requiredWait && r.profile == profile {
		return r.environments, r.problem
	}
	where, err := e.Where(ctx, WhereRequest{Build: profile})
	r.environments, r.problem = where.Environments, ""
	if err != nil {
		r.environments, r.problem = nil, fmt.Sprintf("build.default's profile, %s, couldn't be resolved, so only the environments its checks planned are required: %v", profile, err)
	}
	if ctx.Err() == nil {
		r.at, r.profile = time.Now(), profile
	}
	return r.environments, r.problem
}

// requiredWait is how long one resolution of check.on's environments
// stands within an engine.
const requiredWait = time.Minute

// AcceptedFailure is why a failed port's failure is accepted
// (branchstate.AcceptedFailure).
type AcceptedFailure = branchstate.AcceptedFailure

// The kinds of reason a failure is accepted for (branchstate).
const (
	AcceptedAtBase        = branchstate.AcceptedAtBase
	AcceptedUnestablished = branchstate.AcceptedUnestablished
	AcceptedBlocked       = branchstate.AcceptedBlocked
	AcceptedStanding      = branchstate.AcceptedStanding
)

// StandingAccepted are the failed ports of a check's evidence whose every
// failure a standing decision accepts (standingAccepted), which its report
// names, as submit takes them.
func (e *Engine) StandingAccepted(ctx context.Context, evidence Evidence) (map[string]AcceptedFailure, error) {
	var standing []model.Decision
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		standing, err = r.StandingDecisions(model.FindingEnvironmentFailure)
		return err
	}); err != nil {
		return nil, err
	}
	return branchstate.StandingAccepted(evidence, standing), nil
}

// failsAtBase are the changed ports that failed and couldn't be accepted
// otherwise, whose every failure a finished baseline of the check that
// made it reproduces, in the same environment and at the same phase, each
// with those baselines' names.
func (e *Engine) failsAtBase(ctx context.Context, branch model.BranchID, judged Evidence) (map[string][]string, error) {
	var failing []TargetEvidence
	for _, target := range judged.Failed() {
		if !target.Acceptable() && !target.Missing() && target.Failing() {
			failing = append(failing, target)
		}
	}
	if len(failing) == 0 {
		return nil, nil
	}
	var runs []model.Run
	var base model.ObjectID
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		if runs, err = r.Runs(store.RunFilter{Branch: branch, States: []model.RunState{model.RunPassed, model.RunFailed}}); err != nil {
			return err
		}
		recorded, err := r.Branch(branch)
		base = recorded.Base
		return err
	}); err != nil {
		return nil, err
	}
	baselines := map[model.RunID][]Evidence{}
	read := func(of model.RunID) ([]Evidence, error) {
		if found, ok := baselines[of]; ok {
			return found, nil
		}
		var found []Evidence
		for _, run := range runs {
			if run.BaselineOf != of {
				continue
			}
			base, err := e.RunEvidence(ctx, run.ID)
			if err != nil {
				return nil, err
			}
			found = append(found, base)
		}
		baselines[of] = found
		return found, nil
	}
	atBase := map[string][]string{}
	for _, target := range failing {
		var names []string
		reproduced := true
		for n, cell := range target.Outcomes {
			if cell.Outcome != model.OutcomeFailed {
				continue
			}
			found, err := read(judged.Executions[cell.Execution].Run)
			if err != nil {
				return nil, err
			}
			name := baseFailure(found, target.Target.ID, judged.Plan.Environments[n], cell.Phase)
			// An earlier baseline stands across a rebase where master's
			// build read nothing changed since (Then's item 4).
			if name == "" {
				name, _ = e.standingBaseline(ctx, branch, base, target.Target.ID, judged.Plan.Environments[n], cell.Phase)
			}
			if name == "" {
				reproduced = false
				break
			}
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
		if reproduced && len(names) > 0 {
			atBase[target.Target.Target.Name] = names
		}
	}
	return atBase, nil
}

// baseFailure is the baseline that failed a target in an environment at a
// phase, of those given; empty where none did.
func baseFailure(baselines []Evidence, id model.TargetID, environment model.Environment, phase model.Phase) string {
	for _, base := range baselines {
		n := slices.Index(base.Plan.Environments, environment)
		i := slices.IndexFunc(base.Targets, func(t TargetEvidence) bool { return t.Target.ID == id })
		if n < 0 || i < 0 {
			continue
		}
		if cell := base.Targets[i].Outcomes[n]; cell.Outcome == model.OutcomeFailed && cell.Phase == phase {
			return base.Run.Name()
		}
	}
	return ""
}

// Covered reports whether the finished checks of a branch's tree stand
// for a check asked of it, the plan want proposes: every target want
// builds, in every environment it builds there, passed under a test
// policy at least as strict as want's. A pass on fewer releases, or with
// advisory tests where tests are required, doesn't stand for it; one on
// more, or stricter, does (Codex's review of 832f059e, medium 4). It
// returns the newest check behind them.
func (e *Engine) Covered(ctx context.Context, branch model.BranchID, tree model.ObjectID, want model.Plan) (model.Run, bool, error) {
	judged, found, err := e.EvidenceFor(ctx, branch, tree)
	if err != nil || !found || judged.Unfinished() || len(judged.Failed()) > 0 {
		return model.Run{}, false, err
	}
	return judged.Run, coversPlan(judged.Plan, func(id model.TargetID, environment int) (Cell, bool) {
		for _, target := range judged.Targets {
			if target.Target.ID == id {
				return target.Outcomes[environment], true
			}
		}
		return Cell{}, false
	}, judged.Fetched, want), nil
}

// coversPlan is Covered's judgment: each of want's targets, where want
// builds it, a cell of the evidence that passed and stands under want's
// tests, or one the environment can't build (unmet) either way, with the
// same resolved sources (checks.FinishedSourcesCover), and each Git
// result's own fetch the source want expects: a check whose Git source
// couldn't be read stands for itself alone (change 1 of docs/designs/
// source-comparison.md).
func coversPlan(have model.Plan, cell func(model.TargetID, int) (Cell, bool), fetched func(Cell) model.ObjectID, want model.Plan) bool {
	environments := have.Environments
	if checks.TestsRank(have.Tests) < checks.TestsRank(want.Tests) || !checks.FinishedSourcesCover(have, want) {
		return false
	}
	for _, build := range want.Builds {
		at := slices.Index(environments, build.Environment)
		if at < 0 {
			return false
		}
		for _, id := range build.Order {
			found, ok := cell(id, at)
			switch {
			case !ok:
				return false
			case found.Kind == CellUnmet:
				// The environment can't build it, for this check as for that.
			case found.Kind != CellRecorded || !want.Tests.Stands(found.TargetResult):
				return false
			}
			if source, ok := want.GitIn(build.Environment, id); ok && source.AgainstFetched(fetched(found)) != model.SourceSame {
				return false
			}
		}
	}
	return true
}
