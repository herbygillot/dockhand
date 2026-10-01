package engine

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/reuse"
	"github.com/herbygillot/dockhand/internal/store"
)

// CellKind is what a target's cell of the evidence holds in one
// environment, which its readers take from the cell rather than asking the
// plan again (the code-organization review, finding 25).
type CellKind string

const (
	// CellRecorded is a result an execution recorded, or reused.
	CellRecorded CellKind = "recorded"
	// CellExcluded is a target the plan leaves out there, which needn't
	// pass there.
	CellExcluded CellKind = "excluded"
	// CellUnmet is a target the environment can't build: the cell's Unmet
	// says why, as the plan that found it says.
	CellUnmet CellKind = "unmet"
	// CellNotRun is a target required there that no check of the files
	// built: --only left it out, or the check stopped before it.
	CellNotRun CellKind = "not-run"
	// CellRemade is a target built there before the environment was made
	// again, from another source or with other tools: another environment,
	// whose results don't stand for it (Counts).
	CellRemade CellKind = "remade"
)

// Cell is a target's result in one environment, with what kind of cell it
// is. An excluded, unmet, not run, or remade cell's result is
// OutcomeNotRun, or OutcomeUnmet, with no execution.
type Cell struct {
	model.TargetResult
	Kind        CellKind
	Environment model.Environment
	// Unmet is why the environment can't build the target, for CellUnmet.
	Unmet model.Unmet
	// Recorded is the environment's identity when its result was recorded,
	// for CellRemade, and Change what's other now, in a person's words
	// (RemadeWords).
	Recorded, Change string
}

// recorded is a result recorded in an environment, as a cell: one that
// says its target wasn't reached is a cell not run.
func recorded(environment model.Environment, result model.TargetResult) Cell {
	if result.Outcome == model.OutcomeNotRun {
		return Cell{TargetResult: result, Kind: CellNotRun, Environment: environment}
	}
	return Cell{TargetResult: result, Kind: CellRecorded, Environment: environment}
}

// noResult is a cell of a kind with no result behind it.
func noResult(kind CellKind, environment model.Environment, target model.TargetID) Cell {
	return Cell{TargetResult: model.TargetResult{Target: target, Outcome: model.OutcomeNotRun}, Kind: kind, Environment: environment}
}

// Unbuilt reports a cell whose target no check of the files built there,
// where the plan asks it to be: not run, remade, or unmet.
func (c Cell) Unbuilt() bool {
	return c.Kind == CellNotRun || c.Kind == CellRemade || c.Kind == CellUnmet
}

// TargetEvidence is one planned target's result across a run's
// environments.
type TargetEvidence struct {
	Target model.PlanTarget
	// Outcomes are the target's cells, one for each environment, in the
	// plan's order.
	Outcomes []Cell
	Passed   bool
	// Unchecked is true when no check of the files built the target in an
	// environment it is required in: --only left it out, the check stopped
	// before it, the environment can't build it, or the environment has
	// been made again since.
	Unchecked bool
}

// Remade are the environments where the target's result was recorded
// before the environment was made again, and none since.
func (t TargetEvidence) Remade() []model.Environment {
	var remade []model.Environment
	for _, c := range t.Outcomes {
		if c.Kind == CellRemade {
			remade = append(remade, c.Environment)
		}
	}
	return remade
}

// RemadeWords says what changed in the first environment where the
// target's result was recorded before it changed: its provider's words
// where it can say (buildenv.IdentityExplainer), as Tart says a new guest
// protocol, and otherwise that the environment was made again.
func (t TargetEvidence) RemadeWords() string {
	for _, c := range t.Outcomes {
		if c.Kind != CellRemade {
			continue
		}
		if c.Change != "" {
			return "on " + DescribeEnvironment(c.Environment) + ", " + c.Change
		}
		return DescribeEnvironment(c.Environment) + " was made again, from another source or with other tools"
	}
	return ""
}

// Extra reports a target from --also. It is the one rule for an extra that
// status, submit, and serve read: an extra is built for what it shows, so
// it asks nothing where no check built it, and where it fails, its failure
// is accepted (Acceptable), never fixed.
func (t TargetEvidence) Extra() bool { return t.Target.Role == model.Also }

// Missing reports a target that asks for a check: one no check of the
// files built everywhere it's required, and not an extra.
func (t TargetEvidence) Missing() bool { return t.Unchecked && !t.Extra() }

// Failing reports a target that didn't pass where it must: a changed one
// that didn't pass everywhere it's required, or an extra with a result
// that didn't pass.
func (t TargetEvidence) Failing() bool {
	if t.Passed {
		return false
	}
	if !t.Extra() {
		return true
	}
	return slices.ContainsFunc(t.Outcomes, func(c Cell) bool { return c.Kind == CellRecorded && c.Outcome != model.OutcomePassed })
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
	// Executions are the provider runs the results came from, Run's and
	// Earlier's, by ID.
	Executions map[model.ExecutionID]model.GuestExecution
	// policies are the test policies Run's and Earlier's results were
	// judged under, by check.
	policies map[model.RunID]model.TestPolicy
	// now are the environments' identities as they are now, which the
	// results' own are compared with (Counts).
	now identities
	// origins are the executions that built reused results, by ID, with
	// the checks they were in (decision 28).
	origins map[model.ExecutionID]origin
	// sources are what the builds of an earlier check's results read of
	// the sources this check expects to fetch with Git, by execution and
	// target (readSources), which fill judges them by (Counts).
	sources map[[2]string]readSource
}

// readSource is what an earlier result's build read of the sources a
// newer check expects to fetch with Git: the commit it fetched, where it
// said, and whether it was built against another source of a Git-fetched
// target than the newer check expects (reuse.AgainstOtherSources).
type readSource struct {
	fetched model.ObjectID
	other   bool
}

// origin is an execution that built a result another execution reuses,
// and the check it was in.
type origin struct {
	execution model.GuestExecution
	check     string
}

// origin loads the execution that built a reused result.
func (e *Evidence) origin(r store.Reader, id model.ExecutionID) error {
	if _, ok := e.origins[id]; ok {
		return nil
	}
	execution, err := r.Execution(id)
	if err != nil {
		return err
	}
	run, err := r.Run(execution.Run)
	if err != nil {
		return err
	}
	if e.origins == nil {
		e.origins = map[model.ExecutionID]origin{}
	}
	e.origins[id] = origin{execution: execution, check: run.Name()}
	return nil
}

// Built are the provider runs that built an environment's results, among
// runs: a result a run reused is shown by the run that built it, the one a
// reviewer can look at, with the check that reused it (reusedIn). A run
// shows only for the results it built itself.
func (e Evidence) Built(environment int, runs []model.GuestExecution) (built []model.GuestExecution, reusedIn map[model.ExecutionID]string) {
	reusedIn = map[model.ExecutionID]string{}
	add := func(run model.GuestExecution) {
		if !slices.ContainsFunc(built, func(other model.GuestExecution) bool { return other.ID == run.ID }) {
			built = append(built, run)
		}
	}
	for _, run := range runs {
		for _, target := range e.Targets {
			if environment >= len(target.Outcomes) {
				continue
			}
			result := target.Outcomes[environment]
			if result.Execution != run.ID {
				continue
			}
			if found, ok := e.origins[result.ReusedFrom]; ok {
				add(found.execution)
				reusedIn[found.execution.ID] = e.Checks()[run.Run]
				continue
			}
			add(run)
		}
	}
	slices.SortStableFunc(built, func(a, b model.GuestExecution) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return built, reusedIn
}

// Words is how one target's result in one environment reads, on the
// terminal and in the pull request alike (TargetWords). A result's tests
// read under the policy of the check that built it: when that check is an
// earlier one whose policy differs from this evidence's own, it is named,
// "tests failed (advisory, check-3)", so a later check with --tests
// required never makes an earlier advisory result read as required.
func (e Evidence) Words(target TargetEvidence, environment int, accepted bool) string {
	result := target.Outcomes[environment]
	return targetWords(e.Plan, target.Target, result, e.testsReading(result.TargetResult), accepted)
}

// testsReading says how a result's tests count: "advisory", "not counted,
// --tests skip", and the earlier check it came from when that check's
// policy differs from this evidence's.
func (e Evidence) testsReading(result model.TargetResult) string {
	policy, run := e.Plan.Tests, e.Run.ID
	if execution, ok := e.Executions[result.Execution]; ok {
		run = execution.Run
		if found, ok := e.policies[run]; ok {
			policy = found
		}
	}
	words := "advisory"
	if policy == model.TestsSkip {
		words = "not counted, --tests skip"
	}
	if run != e.Run.ID && policy != e.Plan.Tests {
		if name, ok := e.Checks()[run]; ok {
			words += ", " + name
		}
	}
	return words
}

// Runs are the provider runs behind one environment's results, in the
// plan's order of environments, oldest first: usually one, or an earlier
// attempt's too when it finished some targets, or an earlier check's.
func (e Evidence) Runs(environment int) []model.GuestExecution {
	var runs []model.GuestExecution
	for _, target := range e.Targets {
		if environment >= len(target.Outcomes) {
			continue
		}
		execution, ok := e.Executions[target.Outcomes[environment].Execution]
		if ok && !slices.ContainsFunc(runs, func(run model.GuestExecution) bool { return run.ID == execution.ID }) {
			runs = append(runs, execution)
		}
	}
	slices.SortStableFunc(runs, func(a, b model.GuestExecution) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return runs
}

// Tested reports whether anything the evidence holds was built or reused
// in an environment: whether any of its results came from a provider run
// there. One where every target is excluded has none, and neither has one
// no check reached.
func (e Evidence) Tested(environment int) bool {
	return len(e.Runs(environment)) > 0
}

// ExcludesAll reports whether the plan leaves every one of the evidence's
// targets out in an environment, so that nothing is built there.
func (e Evidence) ExcludesAll(environment int) bool {
	for _, target := range e.Targets {
		if !e.Plan.Excludes(target.Target, e.Plan.Environments[environment]) {
			return false
		}
	}
	return len(e.Targets) > 0
}

// Checks name the checks the evidence's runs were in, check-11, by ID,
// and the checks that built what they reused.
func (e Evidence) Checks() map[model.RunID]string {
	checks := map[model.RunID]string{e.Run.ID: e.Run.Name()}
	for _, run := range e.Earlier {
		checks[run.ID] = run.Name()
	}
	for _, found := range e.origins {
		checks[found.execution.Run] = found.check
	}
	return checks
}

// Observation is what an environment reported about itself, with the
// provider runs that reported it.
type Observation struct {
	Observed model.Observed
	Runs     []model.GuestExecution
}

// Observations are what an environment's runs reported, each report with
// the runs that made it, oldest first: one where they agree, and one each
// where results came from runs that found the environment otherwise, such
// as an earlier check's with other tools. A run that reported nothing
// joins them where there is one report, rather than stand alone.
func (e Evidence) Observations(environment int) []Observation {
	var observations []Observation
	var silent []model.GuestExecution
	for _, run := range e.Runs(environment) {
		if run.Observed == (model.Observed{}) {
			silent = append(silent, run)
			continue
		}
		i := slices.IndexFunc(observations, func(o Observation) bool { return o.Observed == run.Observed })
		if i < 0 {
			observations = append(observations, Observation{Observed: run.Observed})
			i = len(observations) - 1
		}
		observations[i].Runs = append(observations[i].Runs, run)
	}
	switch {
	case len(silent) == 0:
	case len(observations) == 1:
		observations[0].Runs = append(silent, observations[0].Runs...)
		slices.SortStableFunc(observations[0].Runs, func(a, b model.GuestExecution) int { return a.CreatedAt.Compare(b.CreatedAt) })
	default:
		observations = append(observations, Observation{Runs: silent})
	}
	return observations
}

// Recorded reports whether the check itself recorded a result, rather
// than only earlier checks of its files: whether it finished anything.
func (e Evidence) Recorded() bool {
	for _, execution := range e.Executions {
		if execution.Run == e.Run.ID {
			return true
		}
	}
	return false
}

// Missing lists the targets that ask for a check (TargetEvidence.Missing).
func (e Evidence) Missing() []TargetEvidence {
	var missing []TargetEvidence
	for _, target := range e.Targets {
		if target.Missing() {
			missing = append(missing, target)
		}
	}
	return missing
}

// Failed lists the targets that didn't pass where they must
// (TargetEvidence.Failing).
func (e Evidence) Failed() []TargetEvidence {
	var failed []TargetEvidence
	for _, target := range e.Targets {
		if target.Failing() {
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
	var runs []model.Run
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		runs, err = treeRuns(r, branch, tree)
		return err
	}); err != nil || len(runs) == 0 {
		return Evidence{}, false, err
	}
	evidence, err := e.evidenceNow(ctx, runs[0], runs)
	return evidence, err == nil, err
}

// identities are environments' identities by origin now, as their
// providers say (buildenv.IdentityProvider); empty, or absent, for one
// whose provider can't say.
type identities map[model.Environment]string

// identitiesNow asks each environment's provider what it is now. It is
// asked outside any transaction, since a transaction never calls a
// provider; one that can't say leaves the environment's identity unknown.
func (e *Engine) identitiesNow(ctx context.Context, environments []model.Environment) identities {
	now := identities{}
	for _, environment := range environments {
		if identity, err := e.identityNow(ctx, environment); err == nil && identity != "" {
			now[environment] = identity
		}
	}
	return now
}

// identityNow is what an environment's provider says it is now: empty
// where the provider can't say, and an error where it couldn't be read.
func (e *Engine) identityNow(ctx context.Context, environment model.Environment) (string, error) {
	provider, ok := e.Providers[environment.Provider].(buildenv.IdentityProvider)
	if !ok {
		return "", nil
	}
	return provider.Identity(ctx, environment)
}

// evidenceNow is treeEvidence as it stands now: the environments' current
// identities are read first, then the evidence judged by them.
func (e *Engine) evidenceNow(ctx context.Context, primary model.Run, runs []model.Run) (Evidence, error) {
	var environments []model.Environment
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		plan, err := r.Plan(primary.Plan)
		environments = plan.Environments
		return err
	}); err != nil {
		return Evidence{}, err
	}
	now := e.identitiesNow(ctx, environments)
	var evidence Evidence
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		evidence, err = treeEvidence(r, primary, runs, now)
		return err
	})
	// What changed where a result no longer stands is its provider's to
	// say, outside the transaction, as identities are read.
	for t := range evidence.Targets {
		for i, c := range evidence.Targets[t].Outcomes {
			if c.Kind != CellRemade || c.Recorded == "" {
				continue
			}
			if explainer, ok := e.Providers[c.Environment.Provider].(buildenv.IdentityExplainer); ok {
				evidence.Targets[t].Outcomes[i].Change = explainer.IdentityChange(c.Environment, c.Recorded, now[c.Environment])
			}
		}
	}
	return evidence, err
}

// treeRuns lists a branch's finished checks of a tree, newest first. A
// baseline is evidence about another run, never the branch's own check.
func treeRuns(r store.Reader, branch model.BranchID, tree model.ObjectID) ([]model.Run, error) {
	return runsOfTree(r, branch, tree, model.RunPassed, model.RunFailed, model.RunAttention)
}

// runsOfTree are a branch's checks of a tree in the given states, newest
// first, baselines aside.
func runsOfTree(r store.Reader, branch model.BranchID, tree model.ObjectID, states ...model.RunState) ([]model.Run, error) {
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
	runs, err := r.Runs(store.RunFilter{Branch: branch, States: states})
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(runs, func(run model.Run) bool { return !matching[run.Revision] || run.BaselineOf != "" }), nil
}

// treeEvidence is what a run established, judged against everything its
// plan required, the changed targets --only left out included, with the
// gaps filled from earlier checks of the same files, newest first. What
// no check built is unchecked (Design v3 §7: a narrowed check never
// quietly shrinks what submit requires). A result recorded in an
// environment that is another now, by its identity, doesn't count (Counts),
// and nor does an earlier check's blocked result whose blocker's result
// doesn't (standing). The run's own blocked results were recorded in the
// attempt that recorded what blocked them, after its provider
// (blockRemaining), so they stand and fall with it.
func treeEvidence(r store.Reader, primary model.Run, runs []model.Run, now identities) (Evidence, error) {
	plan, err := r.Plan(primary.Plan)
	if err != nil {
		return Evidence{}, err
	}
	evidence, err := runEvidence(r, primary, plan)
	if err != nil {
		return Evidence{}, err
	}
	evidence.now = now
	evidence.dropRemade()
	for _, target := range plan.Omitted {
		te := TargetEvidence{Target: target}
		for _, environment := range plan.Environments {
			kind := CellNotRun
			if plan.Excludes(target, environment) {
				kind = CellExcluded
			}
			te.Outcomes = append(te.Outcomes, noResult(kind, environment, target.ID))
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
		if err := earlier.readSources(r, plan); err != nil {
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
		for _, c := range target.Outcomes {
			if c.Kind == CellNotRun || c.Kind == CellRemade {
				return true
			}
		}
	}
	return false
}

// Counts is the one rule for whether a check's result stands for a target
// in an environment, among checks of the same files (treeRuns):
//   - the check ran in that environment, the whole environment, its
//     developer tools included, and its plan had the target in that
//     environment's order. What it found there stands, an unmet need
//     included;
//   - the environment is the one there is now: its identity when the
//     execution began (recorded) is its identity now, where its provider
//     says what that is (decision 28). An image made again from another
//     source, or with other tools, is another environment;
//   - where the newest check expects the target's build to fetch a commit
//     with Git (git), the result's build recorded fetching that commit
//     (fetched), since the same files name a tag, which binds nothing
//     (batch 20). One recorded without it, as every result was before,
//     or whose provider couldn't say, doesn't count; nor does one whose tag
//     named another commit then. An unmet need, which no execution
//     recorded, says nothing of the source;
//   - it wasn't built against another source of a Git-fetched target than
//     the newest check expects, directly or through another target's
//     build (other), by the rule reuse rebuilds by
//     (reuse.AgainstOtherSources): what was built against a tag's old
//     commit is another build than one against its new one, whatever the
//     files say;
//   - its own check didn't find its source moved: that fetch failed and
//     built nothing, so it stands for no check, even one that expects the
//     commit it fetched.
//
// A blocked result stands besides only with what blocked it: each result
// of its own check that blocked it must stand too, in the same evidence
// (standing). That is a question of the other targets' cells, which fill
// answers as it takes an earlier check's results.
//
// Nothing else about the check's selection matters: from the same files,
// a target builds the same whichever ports were selected with it. Nor does
// its test policy: a result keeps the policy of the check that recorded it,
// and reads under it (decision D1, Evidence.Words).
func Counts(recorded model.Plan, execution model.GuestExecution, id model.TargetID, now string, git *model.GitSource, fetched model.ObjectID, other bool) bool {
	planned, ok := recorded.In(execution.Environment)
	own, fetchedWithGit := recorded.GitIn(execution.Environment, id)
	return ok && planned.Builds(id) && current(execution, now) && (git == nil || execution.ID == "" || git.BuiltBy(fetched)) && !other &&
		!(fetchedWithGit && own.Moved(fetched))
}

// current reports whether an execution ran in the environment there is
// now, by its identity. One whose provider can't say what it is now is
// taken as it was, and so is a result no execution recorded, such as an
// unmet need, which is the plan's.
func current(execution model.GuestExecution, now string) bool {
	return execution.ID == "" || now == "" || execution.Identity == now
}

// dropRemade drops the results recorded in an environment that is another
// now, by its identity, and marks their cells remade: they are the
// environment's as it was, which no longer stands for it.
func (e *Evidence) dropRemade() {
	for t := range e.Targets {
		target := &e.Targets[t]
		for i, result := range target.Outcomes {
			execution, ok := e.Executions[result.Execution]
			if !ok || current(execution, e.now[execution.Environment]) {
				continue
			}
			target.Outcomes[i] = noResult(CellRemade, result.Environment, result.Target)
			target.Outcomes[i].Recorded = execution.Identity
		}
	}
}

// resultKey names a recorded result: the execution that recorded it, and
// its target.
func resultKey(result model.TargetResult) [2]string {
	return [2]string{string(result.Execution), string(result.Target)}
}

// blockers are what blocked each of one check's blocked results, by result
// (resultKey): that check's results there of what the target needs, as its
// plan says, that didn't pass, as a build is blocked
// (buildenv.Build.Blocked). The evidence is the check's own (runEvidence),
// before anything is dropped from it or filled into it.
func (e Evidence) blockers() map[[2]string][][2]string {
	blockers := map[[2]string][][2]string{}
	for _, target := range e.Targets {
		for j, c := range target.Outcomes {
			if c.Kind != CellRecorded || c.Outcome != model.OutcomeBlocked {
				continue
			}
			for _, need := range e.Plan.DependsOnIn(c.Environment, target.Target.ID) {
				k := slices.IndexFunc(e.Targets, func(other TargetEvidence) bool { return other.Target.ID == need })
				if k < 0 {
					continue
				}
				if found := e.Targets[k].Outcomes[j]; found.Execution != "" && found.Outcome != model.OutcomePassed {
					blockers[resultKey(c.TargetResult)] = append(blockers[resultKey(c.TargetResult)], resultKey(found.TargetResult))
				}
			}
		}
	}
	return blockers
}

// standing keeps among stands, results by key, only the blocked results
// each of whose blockers stands too, in turn: a blocked result built
// nothing, and says only that what it needs didn't pass, so it stands
// only with the results that said so. A tag moved since leaves an earlier
// check's failure of a Git-fetched target standing for no later check, and
// what it blocked with it; a later result of the blocker, as a narrowed
// check's pass, leaves the block standing for nothing. A blocked result
// whose check names nothing that blocked it, as a provider can block a
// target of its own accord, stands as it is.
func standing(stands map[[2]string]bool, blockers map[[2]string][][2]string) {
	for dropped := true; dropped; {
		dropped = false
		for blocked, by := range blockers {
			if stands[blocked] && slices.ContainsFunc(by, func(key [2]string) bool { return !stands[key] }) {
				delete(stands, blocked)
				dropped = true
			}
		}
	}
}

// readSources reads what an earlier check's builds read of the sources a
// newer check expects to fetch with Git (primary), for fill to judge them
// by (Counts): the commit each recorded fetching, from its inputs, and
// which were built against another source of a Git-fetched target than
// the newer check expects, directly or through another target's build, by
// the rule reuse rebuilds by (reuse.AgainstOtherSources). Within a check, a
// target's build read that check's result of what it needs there: the
// build it made, or the earlier build it reused, installed from its kept
// archive, whose result carries that build's archive and inputs. A result
// with no inputs recorded read nothing dockhand can name, so what it needs
// of a Git-fetched target can't be established. A blocked result built
// nothing, so the rule has nothing of it to judge: it stands with what
// blocked it (standing). Nothing is read where the newer check fetches
// nothing with Git.
func (e *Evidence) readSources(r store.Reader, primary model.Plan) error {
	if !slices.ContainsFunc(primary.Builds, func(build model.EnvironmentPlan) bool { return len(build.Git) > 0 }) {
		return nil
	}
	e.sources = map[[2]string]readSource{}
	for j, environment := range e.Plan.Environments {
		var targets []reuse.Target
		built := map[model.TargetID]reuse.Candidate{}
		keys := map[model.TargetID][2]string{}
		for _, target := range e.Targets {
			c := target.Outcomes[j]
			if c.Kind != CellRecorded {
				continue
			}
			var inputs model.TargetInputs
			if c.Inputs != "" {
				var err error
				if inputs, err = r.Inputs(c.Inputs); err != nil {
					return err
				}
			}
			build := reuse.Candidate{Result: c.TargetResult, Inputs: inputs}
			planned := reuse.Target{PlanTarget: target.Target, DependsOn: e.Plan.DependsOnIn(environment, target.Target.ID), Earlier: []reuse.Candidate{build}}
			if source, ok := primary.GitIn(environment, target.Target.ID); ok {
				planned.Git = &source
			}
			targets = append(targets, planned)
			if c.Outcome == model.OutcomePassed || c.Outcome == model.OutcomeFailed {
				built[target.Target.ID] = build
			}
			keys[target.Target.ID] = resultKey(c.TargetResult)
			e.sources[keys[target.Target.ID]] = readSource{fetched: inputs.Fetched}
		}
		// A Git-fetched target the earlier check has no result of, which
		// a build there had active all the same, was a build dockhand
		// can't place.
		expected, _ := primary.In(environment)
		for _, id := range slices.Sorted(maps.Keys(expected.Git)) {
			if _, ok := keys[id]; !ok {
				source := expected.Git[id]
				targets = append(targets, reuse.Target{PlanTarget: model.PlanTarget{ID: id}, Git: &source})
			}
		}
		for id := range reuse.AgainstOtherSources(targets, built) {
			read := e.sources[keys[id]]
			read.other = true
			e.sources[keys[id]] = read
		}
	}
	return nil
}

// fill takes an earlier check's results for what this evidence lacks,
// where they count (Counts), and reports whether it took any. A blocked
// result is taken only with the results of that check that blocked it
// (standing): this evidence's cells for them hold either those, taken
// here, or another result.
func (e *Evidence) fill(earlier Evidence) bool {
	type place struct {
		t, i int
		cell Cell
	}
	var found []place
	for t := range e.Targets {
		target := &e.Targets[t]
		k := slices.IndexFunc(earlier.Targets, func(other TargetEvidence) bool { return other.Target.ID == target.Target.ID })
		if k < 0 {
			continue
		}
		for i, result := range target.Outcomes {
			environment := result.Environment
			if result.Kind != CellNotRun && result.Kind != CellRemade {
				continue
			}
			j := slices.Index(earlier.Plan.Environments, environment)
			if j < 0 {
				continue
			}
			cell := earlier.Targets[k].Outcomes[j]
			execution, recorded := earlier.Executions[cell.Execution]
			if !recorded {
				// An unmet result is the plan's, with no execution behind it.
				execution = model.GuestExecution{Environment: environment}
			}
			var git *model.GitSource
			if source, ok := e.Plan.GitIn(environment, target.Target.ID); ok {
				git = &source
			}
			read := earlier.sources[resultKey(cell.TargetResult)]
			if cell.Kind != CellRecorded && cell.Kind != CellUnmet || !Counts(earlier.Plan, execution, target.Target.ID, e.now[environment], git, read.fetched, read.other) {
				if cell.Kind == CellRecorded && !current(execution, e.now[environment]) {
					target.Outcomes[i].Kind, target.Outcomes[i].Recorded = CellRemade, execution.Identity
				}
				continue
			}
			found = append(found, place{t: t, i: i, cell: cell})
		}
	}
	stands := map[[2]string]bool{}
	for _, p := range found {
		if p.cell.Kind == CellRecorded {
			stands[resultKey(p.cell.TargetResult)] = true
		}
	}
	standing(stands, earlier.blockers())
	took := false
	for _, p := range found {
		if p.cell.Kind == CellRecorded && !stands[resultKey(p.cell.TargetResult)] {
			continue
		}
		e.Targets[p.t].Outcomes[p.i] = p.cell
		if builder, ok := earlier.origins[p.cell.ReusedFrom]; ok {
			if e.origins == nil {
				e.origins = map[model.ExecutionID]origin{}
			}
			e.origins[p.cell.ReusedFrom] = builder
		}
		if execution, recorded := earlier.Executions[p.cell.Execution]; recorded {
			if e.Executions == nil {
				e.Executions = map[model.ExecutionID]model.GuestExecution{}
			}
			e.Executions[execution.ID] = execution
			if policy, ok := earlier.policies[execution.Run]; ok {
				if e.policies == nil {
					e.policies = map[model.RunID]model.TestPolicy{}
				}
				e.policies[execution.Run] = policy
			}
		}
		took = true
	}
	return took
}

// settle works out each target's verdict from its cells: passed where it
// passed in every environment it is required in, and unchecked where one
// has no result. A cell an earlier check filled is recorded, or unmet, so
// it is remade no longer.
func (e *Evidence) settle() {
	for t := range e.Targets {
		target := &e.Targets[t]
		target.Passed, target.Unchecked = true, false
		for _, c := range target.Outcomes {
			if c.Kind == CellExcluded {
				continue
			}
			target.Passed = target.Passed && c.Outcome == model.OutcomePassed
			target.Unchecked = target.Unchecked || c.Unbuilt()
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
		unmet, needs := target.unmet()
		remade := target.Remade()
		switch {
		case needs && target.Missing():
			problems = append(problems, fmt.Sprintf("%s %s, which %s hasn't; a check with %s there builds it, or share the branch as a draft (--draft)",
				name, UnmetWords(unmet), DescribeEnvironment(unmet.Environment), unmet.Needs))
		case len(remade) > 0 && target.Missing():
			problems = append(problems, fmt.Sprintf("%s's check no longer stands: since it, %s; dockhand check builds it there again, or share the branch as a draft (--draft)", name, target.RemadeWords()))
		case target.Missing():
			problems = append(problems, fmt.Sprintf("%s is changed, and no check of these files built it everywhere it's required; dockhand check builds it, or share the branch as a draft (--draft)", name))
		case !Acceptable(target.Target):
			problems = append(problems, fmt.Sprintf("%s did not pass in %s; fix it, or share it as a draft (--draft)", name, evidence.Run.Name()))
		case !slices.Contains(accepted, name):
			problems = append(problems, fmt.Sprintf("%s (%s) did not pass in %s; acknowledge it with --accept %s if its failure is not this branch's doing", name, kindWords(target.Target), evidence.Run.Name(), name))
		}
	}
	return problems
}

// unmet is the first environment that can't build a target, when one
// can't, and it has no result from an earlier check there either, as the
// plan that found it says.
func (t TargetEvidence) unmet() (model.Unmet, bool) {
	for _, c := range t.Outcomes {
		if c.Kind == CellUnmet {
			return c.Unmet, true
		}
	}
	return model.Unmet{}, false
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

// VariantsBuilt are the variant builds a --variants each check made of its
// port, as MacPorts writes each, "+tests", with whether every one passed
// wherever it's required, and its default build too: what the pull
// request template's variants item asks, which such a check answers.
func (e Evidence) VariantsBuilt() (port string, builds []string, passed bool) {
	if !e.Plan.EachVariant {
		return "", nil, false
	}
	passed = true
	for _, target := range e.Targets {
		if spec := target.Target.Target.VariantSpec(); spec != "" {
			port = target.Target.Target.Name
			builds = append(builds, spec)
		}
	}
	for _, target := range e.Targets {
		if target.Target.Target.Name == port && !target.Passed {
			passed = false
		}
	}
	return port, builds, passed && len(builds) > 0
}
