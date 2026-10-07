// Package evidence says what recorded checks establish about a tree in
// the environments required now (the architecture review's finding 4): each
// planned target's result in each environment, as a cell that says what it
// is; which of an earlier check's results still stand for a newer one
// (Counts), a blocked result only with what blocked it; and each target's
// verdict. It is pure, as planning and reuse are: the engine loads the
// records (Check) and asks providers what each environment is now
// (Identities), outside any transaction, and evidence combines what it's
// given. What a cell reads as, in a person's words, is the engine's.
//
// It is distinct from reuse: reusing a build across trees needs its whole
// recorded inputs, where evidence from checks of one tree has its own
// allowances. They share the rule by which a build was made against
// another source of a Git-fetched target (reuse.AgainstOtherSources).
package evidence

import (
	"maps"
	"slices"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/reuse"
)

// Identities are environments' identities now, as their providers say
// (buildenv.IdentityProvider); absent for one whose provider can't say.
type Identities map[model.Environment]string

// Check is one finished check's record, as the engine loads it: its run,
// its plan, its executions and each one's results, the executions that
// built the results it reused, with the checks they were in, and what its
// results' builds read (model.TargetInputs), by their keys, which is
// needed only where a newer check fetches a target with Git (ReadsSources).
type Check struct {
	Run        model.Run
	Plan       model.Plan
	Executions []model.GuestExecution
	Results    map[model.ExecutionID][]model.TargetResult
	Origins    map[model.ExecutionID]Origin
	Inputs     map[string]model.TargetInputs
}

// Origin is an execution that built a result another execution reuses,
// and the check it was in.
type Origin struct {
	Execution model.GuestExecution
	Check     string
}

// ReadsSources reports whether judging earlier checks for a plan needs
// what their builds read: whether it fetches any target with Git.
func ReadsSources(plan model.Plan) bool {
	return slices.ContainsFunc(plan.Builds, func(build model.EnvironmentPlan) bool { return len(build.Git) > 0 })
}

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
	// Unmet is why the environment can't build the target, for CellUnmet,
	// and Exclusion why the plan leaves it out there, for CellExcluded.
	Unmet     model.Unmet
	Exclusion model.Exclusion
	// Recorded is the environment's identity when its result was recorded,
	// for CellRemade, and Change what's other now, in a person's words
	// (RemadeWords).
	Recorded, Change string
	// NotReusable is why no later check can reuse a passed result, where
	// what its build read wasn't recorded (model.TargetInputs).
	NotReusable string
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

// Reason is why a recorded result didn't wholly pass, in its provider's
// words, where it said: for a build that failed, or one that passed with
// its tests failing, which count for nothing under an advisory policy and
// so say nothing more than "tests failed" otherwise. The rust run, #35084,
// read "tests failed (advisory)" on both its releases, where its bootstrap
// had panicked before any test ran. Empty for anything else: a timeout's
// reason is its deadline, which its words say, and a blocked target's
// failed dependency has a row of its own.
func (c Cell) Reason() string {
	if c.Kind != CellRecorded {
		return ""
	}
	switch {
	case c.Outcome == model.OutcomeFailed, c.Outcome == model.OutcomePassed && c.Tests == model.TestsFailed:
		return c.Detail
	}
	return ""
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
	now Identities
	// origins are the executions that built reused results, by ID, with
	// the checks they were in (decision 28).
	origins map[model.ExecutionID]Origin
	// sources are what the builds of an earlier check's results read of
	// the sources this check expects to fetch with Git, by execution and
	// target (readSources), which fill judges them by (Counts), from
	// inputs, what its results' builds recorded reading, by key.
	sources map[[2]string]readSource
	inputs  map[string]model.TargetInputs
	// Problem is why the environments check.on names, which the evidence
	// always requires, couldn't be resolved, so it requires only those its
	// checks planned; empty where they were.
	Problem string
}

// readSource is what an earlier result's build read of the sources a
// newer check expects to fetch with Git: the commit it fetched, where it
// said, and whether it was built against another source of a Git-fetched
// target than the newer check expects (reuse.AgainstOtherSources).
type readSource struct {
	fetched model.ObjectID
	other   bool
}

// Of is what one check established for each target in each of its
// environments, as its own record says: its executions' results merged,
// an earlier attempt's complete result standing over a later one's, and
// each planned target's cell, excluded or unmet as its plan says, with
// each target's verdict.
func Of(check Check) Evidence {
	plan := check.Plan
	evidence := Evidence{Run: check.Run, Plan: plan, Executions: map[model.ExecutionID]model.GuestExecution{},
		policies: map[model.RunID]model.TestPolicy{check.Run.ID: plan.Tests}, inputs: check.Inputs}
	byEnvironment := map[model.Environment][]model.GuestExecution{}
	for _, execution := range check.Executions {
		evidence.Executions[execution.ID] = execution
		byEnvironment[execution.Environment] = append(byEnvironment[execution.Environment], execution)
	}
	merged := map[model.Environment]map[model.TargetID]model.TargetResult{}
	for environment, executions := range byEnvironment {
		merged[environment] = Merged(executions, check.Results)
	}
	for _, target := range plan.Targets {
		te := TargetEvidence{Target: target, Passed: true}
		for _, environment := range plan.Environments {
			if exclusion, ok := plan.ExclusionIn(environment, target.ID); ok {
				cell := noResult(CellExcluded, environment, target.ID)
				cell.Exclusion = exclusion
				te.Outcomes = append(te.Outcomes, cell)
				continue
			}
			if unmet, ok := plan.UnmetIn(environment, target.ID); ok {
				te.Outcomes = append(te.Outcomes, Cell{TargetResult: model.TargetResult{Target: target.ID, Outcome: model.OutcomeUnmet}, Kind: CellUnmet, Environment: environment, Unmet: unmet})
				te.Passed = false
				continue
			}
			result, ok := merged[environment][target.ID]
			if !ok {
				te.Outcomes = append(te.Outcomes, noResult(CellNotRun, environment, target.ID))
				te.Passed = false
				continue
			}
			te.Outcomes = append(te.Outcomes, recorded(environment, result))
			te.Passed = te.Passed && result.Outcome == model.OutcomePassed
			if found, ok := check.Origins[result.ReusedFrom]; ok && result.ReusedFrom != "" {
				if evidence.origins == nil {
					evidence.origins = map[model.ExecutionID]Origin{}
				}
				evidence.origins[result.ReusedFrom] = found
			}
		}
		evidence.Targets = append(evidence.Targets, te)
	}
	evidence.settle()
	return evidence
}

// Merged are one environment's results across a check's attempts, oldest
// first: a later attempt's result replaces an earlier one's unless the
// earlier one is a complete verdict, which is final.
func Merged(executions []model.GuestExecution, results map[model.ExecutionID][]model.TargetResult) map[model.TargetID]model.TargetResult {
	merged := map[model.TargetID]model.TargetResult{}
	executions = slices.Clone(executions)
	slices.SortFunc(executions, func(a, b model.GuestExecution) int { return a.Attempt - b.Attempt })
	for _, execution := range executions {
		for _, result := range results[execution.ID] {
			if earlier, ok := merged[result.Target]; ok && earlier.Outcome.Complete() {
				continue
			}
			merged[result.Target] = result
		}
	}
	return merged
}

// Judge is what a tree's checks established, judged against everything
// the plan requires (Plan), the changed targets the primary check's
// --only left out included, with the gaps filled from earlier checks of
// the same files, newest first. What no check built is unchecked (Design
// v3 §7: a narrowed check never quietly shrinks what submit requires). A
// result recorded in an environment that is another now, by its identity,
// doesn't count (Counts), and nor does an earlier check's blocked result
// whose blocker's result doesn't (standing). The primary check's own
// blocked results were recorded in the attempt that recorded what blocked
// them, after its provider (blockRemaining), so they stand and fall with
// it.
func Judge(primary Check, plan model.Plan, earlier []Check, now Identities) Evidence {
	primary.Plan = plan
	evidence := Of(primary)
	evidence.now = now
	evidence.dropRemade()
	for _, target := range plan.Omitted {
		te := TargetEvidence{Target: target}
		for _, environment := range plan.Environments {
			cell := noResult(CellNotRun, environment, target.ID)
			if exclusion, ok := plan.ExclusionIn(environment, target.ID); ok {
				cell = noResult(CellExcluded, environment, target.ID)
				cell.Exclusion = exclusion
			}
			te.Outcomes = append(te.Outcomes, cell)
		}
		evidence.Targets = append(evidence.Targets, te)
	}
	for _, check := range earlier {
		if check.Run.ID == primary.Run.ID || !evidence.missing() {
			continue
		}
		found := Of(check)
		found.readSources(plan)
		if evidence.fill(found) {
			evidence.Earlier = append(evidence.Earlier, check.Run)
		}
	}
	evidence.settle()
	return evidence
}

// Plan is the plan a tree's evidence is judged against: the primary
// check's, with each environment another check of the same files planned,
// by the newest that did (others, newest first), and the required ones,
// check.on's, which no check may have planned (D17). A check narrowed to
// one release replaced the others' results: sand-runner's check on macOS
// 15 alone left its pass on 26 out of status and submit (the sand-runner
// port). An environment no check planned has no plan of its own, and what
// it requires is unchecked there.
func Plan(primary model.Plan, others []model.Plan, required []model.Environment) model.Plan {
	plan := primary
	plan.Environments = slices.Clone(plan.Environments)
	plan.Builds = slices.Clone(plan.Builds)
	for _, earlier := range others {
		for _, build := range earlier.Builds {
			if !slices.Contains(plan.Environments, build.Environment) {
				plan.Environments = append(plan.Environments, build.Environment)
				plan.Builds = append(plan.Builds, build)
			}
		}
	}
	for _, environment := range required {
		if !slices.ContainsFunc(plan.Environments, func(planned model.Environment) bool { return Covers(environment, planned) }) {
			plan.Environments = append(plan.Environments, environment)
		}
	}
	return plan
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
				add(found.Execution)
				reusedIn[found.Execution.ID] = e.Checks()[run.Run]
				continue
			}
			add(run)
		}
	}
	slices.SortStableFunc(built, func(a, b model.GuestExecution) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return built, reusedIn
}

// TestsReading says how a result's tests count, under the policy of the
// check that recorded it (D1): "advisory", "not counted,
// --tests skip", and the earlier check it came from when that check's
// policy differs from this evidence's.
func (e Evidence) TestsReading(result model.TargetResult) string {
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
		checks[found.Execution.Run] = found.Check
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
		if run.Observed.IsZero() {
			silent = append(silent, run)
			continue
		}
		i := slices.IndexFunc(observations, func(o Observation) bool { return o.Observed.Equal(run.Observed) })
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

// ReusedAll names the earlier checks whose builds the check reused, when it
// built nothing itself: every one of its own executions reused every
// target's result (decision 28). It is empty for a check that built
// anything, or recorded nothing.
func (e Evidence) ReusedAll() []string {
	own := map[model.ExecutionID]bool{}
	for _, execution := range e.Executions {
		if execution.Run != e.Run.ID {
			continue
		}
		if !execution.Reused {
			return nil
		}
		own[execution.ID] = true
	}
	var checks []string
	for _, target := range e.Targets {
		for _, result := range target.Outcomes {
			if !own[result.Execution] {
				continue
			}
			if found, ok := e.origins[result.ReusedFrom]; ok && !slices.Contains(checks, found.Check) {
				checks = append(checks, found.Check)
			}
		}
	}
	slices.Sort(checks)
	return checks
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

// Acceptable reports whether the target's failure may be acknowledged
// with --accept (Acceptable).
func (t TargetEvidence) Acceptable() bool { return Acceptable(t.Target) }

// Covers reports whether a check in planned stands for a default
// environment: the same provider, on the same release and architecture
// where the default names one. Where its tools since differ, as when a
// release has since had its Xcode image made, whether its results still
// stand is the environment's identity's to say (Counts).
func Covers(required, planned model.Environment) bool {
	return required.Provider == planned.Provider && (required.Platform == model.Platform{} || required.Platform == planned.Platform)
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
func (e *Evidence) readSources(primary model.Plan) {
	if !ReadsSources(primary) {
		return
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
			inputs := e.inputs[c.Inputs]
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
				e.origins = map[model.ExecutionID]Origin{}
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

// Unmet is the first environment that can't build a target, when one
// can't, and it has no result from an earlier check there either, as the
// plan that found it says.
func (t TargetEvidence) Unmet() (model.Unmet, bool) {
	for _, c := range t.Outcomes {
		if c.Kind == CellUnmet {
			return c.Unmet, true
		}
	}
	return model.Unmet{}, false
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
