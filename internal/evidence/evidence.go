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
	"time"

	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/reuse"
)

// Identities are environments' identities now, as their providers say
// (buildenv.IdentityProvider): known, with its value; none, where the
// provider can't say, which is the zero Identity or an absent entry; or
// unread, with the cause, where it couldn't be read (#190).
type Identities map[model.Environment]Identity

// Identity is one environment's identity now: Value where it is known,
// Unread the cause where it couldn't be read; neither where its provider
// can't say.
type Identity struct {
	Value  string
	Unread string
}

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
	return slices.ContainsFunc(plan.Builds, func(build model.EnvironmentPlan) bool { return len(build.Git) > 0 || len(build.Tags) > 0 })
}

// AnyReadsSources reports whether any of plans fetches a target with Git
// or by a tag: whether the checks of the files must be loaded with what
// their builds read, an earlier extra's Git build included, which the
// newest plan may not build at all (Codex's review of #192, finding 3).
func AnyReadsSources(plans ...model.Plan) bool {
	return slices.ContainsFunc(plans, ReadsSources)
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
	// built: --only left it out, or the check stopped before it or as it
	// built.
	CellNotRun CellKind = "not-run"
	// CellRemade is a target built there before the environment was made
	// again, from another source or with other tools: another environment,
	// whose results don't stand for it (Counts).
	CellRemade CellKind = "remade"
	// CellEntered is a result recorded after a shell opened in its
	// running guest (model.Run.Entered), which the shell could have
	// touched: no evidence, its outcome kept as EnteredOutcome for words.
	CellEntered CellKind = "entered"
)

// Cell is a target's result in one environment, with what kind of cell it
// is. An excluded, unmet, not run, or remade cell's result is
// OutcomeNotRun, OutcomeInterrupted, or OutcomeUnmet, with no execution.
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
	// PlannedIn is the check that planned a target not run in an
	// environment outside build.default's profile, which asks for it there
	// only because that check did; empty in the profile's own, and where
	// a check's result there was turned down (Refused).
	PlannedIn string
	// Refused is why an earlier check's result for a target not run here
	// doesn't count, as fill found it: that check built it, so its words
	// say why it no longer stands, never that the check stopped before it
	// (#192's change-4 test). Nil where no check's result was turned down.
	Refused *Refusal
	// Unrun is why the environment's build never ran, in its provider's
	// words, for a target not run where its execution finished building
	// nothing: a GitHub job no runner took, "the job never ran: GitHub
	// says …" (buildenv.Unrun).
	Unrun string
	// Entered is the shell opened before a CellEntered result was
	// recorded, and EnteredOutcome the outcome it recorded.
	Entered        *model.Entry
	EnteredOutcome model.Outcome
}

// recorded is a result recorded in an environment, as a cell: one that
// says its target wasn't reached is a cell not run, and so is one that was
// building when its environment was lost, which says nothing of the port,
// and whose execution built nothing the evidence holds.
func recorded(environment model.Environment, result model.TargetResult) Cell {
	switch result.Outcome {
	case model.OutcomeNotRun:
		return Cell{TargetResult: result, Kind: CellNotRun, Environment: environment}
	case model.OutcomeInterrupted:
		result.Execution = ""
		return Cell{TargetResult: result, Kind: CellNotRun, Environment: environment}
	}
	return Cell{TargetResult: result, Kind: CellRecorded, Environment: environment}
}

// noResult is a cell of a kind with no result behind it.
func noResult(kind CellKind, environment model.Environment, target model.TargetID) Cell {
	return Cell{TargetResult: model.TargetResult{Target: target, Outcome: model.OutcomeNotRun}, Kind: kind, Environment: environment}
}

// Refusal is what the judgment found of an earlier check's result that
// doesn't count (Counts, sourceRefusal, standing), for a cell it leaves
// unbuilt: status's cell and submit's hold read it alike.
type Refusal struct {
	// Check is the earlier check whose result it was.
	Check string
	Kind  RefusalKind
	// Ref is the source's tag or Git ref, Then the commit the result's
	// build had and Now the one the ref names now, where the kind has
	// them, either empty where it isn't known; Dependency is the target
	// whose build or result it rested on, for a dependency's or a
	// blocker's kind.
	Ref, Then, Now string
	Dependency     model.TargetID
}

// RefusalKind is why an earlier check's result doesn't count.
type RefusalKind string

const (
	// RefusedMoved is a result built from a tag or Git ref that names
	// another commit now.
	RefusedMoved RefusalKind = "moved"
	// RefusedUnread is one whose source couldn't be read when it was
	// planned, so it stands for that check alone.
	RefusedUnread RefusalKind = "unread"
	// RefusedUnreadNow is one built from a commit (Then) of a ref that
	// couldn't be read now, so whether it still names that commit isn't
	// known.
	RefusedUnreadNow RefusalKind = "unread-now"
	// RefusedUnrecorded is a Git-fetched result whose build didn't say
	// which commit it fetched.
	RefusedUnrecorded RefusalKind = "unrecorded"
	// RefusedOwnMove is one whose own check found its source naming
	// another commit than it planned (Now, not Then): that fetch built
	// nothing that stands.
	RefusedOwnMove RefusalKind = "own-move"
	// RefusedFetchMode is a result built with Git, where the target is
	// now fetched another way.
	RefusedFetchMode RefusalKind = "fetch-mode"
	// RefusedDependency is one built against a dependency whose source
	// has moved since (Ref, Then, Now).
	RefusedDependency RefusalKind = "dependency"
	// RefusedDependencyUnshown is one built against a Git-fetched or
	// tagged dependency that can't be shown to have been the commit it
	// names now (Now, where it's known).
	RefusedDependencyUnshown RefusalKind = "dependency-unshown"
	// RefusedDependencyRefused is one built against a dependency's build
	// that doesn't count itself.
	RefusedDependencyRefused RefusalKind = "dependency-refused"
	// RefusedBlocked is a blocked result whose blocker's result doesn't
	// count.
	RefusedBlocked RefusalKind = "blocked"
	// RefusedNotPlanned is one its check's plan didn't build there.
	RefusedNotPlanned RefusalKind = "not-planned"
	// refusedRemade is one built in an environment made again since,
	// which fill marks CellRemade.
	refusedRemade RefusalKind = "remade"
)

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
	return c.Kind == CellNotRun || c.Kind == CellRemade || c.Kind == CellUnmet || c.Kind == CellEntered
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

// AcceptName is the name a target's failure is accepted, kept, and said
// by: its build's, as the Tested on table names it, "curl +ssl
// +sectrust", whatever its sibling builds did, a bare port name its
// default build's, so accepting a variant build doesn't accept the default
// build (field testing's note 10 from the curl fix). The port's bare name
// is another way to name a variant build, where it's the one build of a
// port with no default build that failed (AcceptTarget, #235).
func (e Evidence) AcceptName(target TargetEvidence) string { return string(target.Target.ID) }

// AcceptTarget is the build a name given to submit --accept takes: the
// build it names, or for a port's bare name where the port has no default
// build, the one build of it that failed. False where it names none, as
// where several of the port's builds failed (FailedBuildsOf names them).
func (e Evidence) AcceptTarget(name string) (TargetEvidence, bool) {
	if i := slices.IndexFunc(e.Targets, func(t TargetEvidence) bool { return string(t.Target.ID) == name }); i >= 0 {
		return e.Targets[i], true
	}
	if failed := e.FailedBuildsOf(name); len(failed) == 1 {
		return failed[0], true
	}
	return TargetEvidence{}, false
}

// Accepts reports whether any of the names given to --accept takes the
// target (AcceptTarget).
func (e Evidence) Accepts(names []string, target TargetEvidence) bool {
	return slices.ContainsFunc(names, func(name string) bool {
		found, ok := e.AcceptTarget(name)
		return ok && found.Target.ID == target.Target.ID
	})
}

// BuildsOf are the builds of a port the evidence has, in the plan's order.
func (e Evidence) BuildsOf(port string) []TargetEvidence {
	var builds []TargetEvidence
	for _, target := range e.Targets {
		if target.Target.Target.Name == port {
			builds = append(builds, target)
		}
	}
	return builds
}

// FailedBuildsOf are the builds of a port that failed where a check built
// them, a build never built not one of them.
func (e Evidence) FailedBuildsOf(port string) []TargetEvidence {
	var failed []TargetEvidence
	for _, target := range e.BuildsOf(port) {
		if target.Failing() && slices.ContainsFunc(target.Outcomes, func(c Cell) bool { return c.Kind == CellRecorded && c.Outcome != model.OutcomePassed }) {
			failed = append(failed, target)
		}
	}
	return failed
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
	Run model.Run
	// Plan is what the checks planned, merged: its Asked and Profile are
	// the newest check's alone. Ask is the check that governs.
	Plan    model.Plan
	Ask     *Ask
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
	// readings are the plan with each environment the newest check didn't
	// plan read by the newest check's resolutions, which an earlier
	// check's results there are judged against (newestReadings).
	readings model.Plan
	// shown are the targets whose tag some earlier check's plan shows moved
	// against the newest reading (shownMoves), which a reading that isn't
	// known doesn't stand beside.
	shown map[model.TargetID]bool
	// Problem is why the environments build.default's profile names, which the evidence
	// always requires, couldn't be resolved, so it requires only those its
	// checks planned; empty where they were.
	Problem string
}

// readSource is what an earlier result's build read of the sources a
// newer check expects to fetch with Git: the commit it fetched, where it
// said, and the dependency through which it was built against another
// source of a Git-fetched target than the newer check expects, where it
// was (reuse.AgainstOtherSources).
type readSource struct {
	fetched model.ObjectID
	against *reuse.Against
}

// OfNow is Of with the environments' identities as they read now, so the
// evidence says which couldn't be read (UnreadIdentities), as a check's
// own report and wait's do (Codex's review of #193, finding 2).
func OfNow(check Check, now Identities) Evidence {
	evidence := Of(check)
	evidence.now = now
	return evidence
}

// Of is what one check established for each target in each of its
// environments, as its own record says: its executions' results merged,
// an earlier attempt's complete result standing over a later one's, and
// each planned target's cell, excluded or unmet as its plan says, with
// each target's verdict.
func Of(check Check) Evidence {
	plan := check.Plan
	evidence := Evidence{Run: check.Run, Plan: plan, Executions: map[model.ExecutionID]model.GuestExecution{},
		policies: map[model.RunID]model.TestPolicy{check.Run.ID: plan.Tests}, inputs: maps.Clone(check.Inputs)}
	byEnvironment := map[model.Environment][]model.GuestExecution{}
	for _, execution := range check.Executions {
		evidence.Executions[execution.ID] = execution
		byEnvironment[execution.Environment] = append(byEnvironment[execution.Environment], execution)
	}
	merged := map[model.Environment]map[model.TargetID]model.TargetResult{}
	// unrun are the environments whose newest execution finished with
	// words and nothing built: their build never ran.
	unrun := map[model.Environment]string{}
	for environment, executions := range byEnvironment {
		merged[environment] = Merged(executions, check.Results)
		newest := slices.MaxFunc(executions, func(a, b model.GuestExecution) int { return a.Attempt - b.Attempt })
		if newest.State == model.ExecutionFinished && newest.Detail != "" && len(check.Results[newest.ID]) == 0 {
			unrun[environment] = newest.Detail
		}
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
				cell := noResult(CellNotRun, environment, target.ID)
				cell.Unrun = unrun[environment]
				te.Outcomes = append(te.Outcomes, cell)
				te.Passed = false
				continue
			}
			// A result recorded after a shell opened in its guest isn't
			// evidence: the shell could have touched it.
			if entry := enteredBefore(check.Run, environment, result); entry != nil {
				cell := noResult(CellEntered, environment, target.ID)
				cell.Entered, cell.EnteredOutcome = entry, result.Outcome
				te.Outcomes = append(te.Outcomes, cell)
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

// enteredBefore is the first shell opened in run's guest in environment
// before result was recorded, by run's own build, nil where none was: a
// result recorded before any shell opened stands, as does one reused from
// another check's build.
func enteredBefore(run model.Run, environment model.Environment, result model.TargetResult) *model.Entry {
	if result.ReusedFrom != "" {
		return nil
	}
	for i, entry := range run.Entered {
		if entry.Environment == environment && entry.At.Before(result.RecordedAt) {
			return &run.Entered[i]
		}
	}
	return nil
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
//
// Outside what the newest check asked for (required), a result counts
// only where it failed or passed (outsideProfile).
func Judge(primary Check, plan model.Plan, earlier []Check, now Identities, required []model.Environment) Evidence {
	checks := append([]Check{primary}, earlier...)
	// An environment only an earlier check planned is judged against the
	// newest check's own reading of each source, where it has one
	// (change 4 of docs/designs/source-comparison.md).
	readings := newestReadings(primary.Plan, plan)
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
	added := evidence.addBuilds(primary, earlier)
	shown := shownMoves(earlier, readings)
	for _, check := range earlier {
		if check.Run.ID == primary.Run.ID || !evidence.missing() {
			continue
		}
		found := Of(check)
		found.readings, found.shown = readings, shown
		found.readSources(readings)
		if evidence.fill(found) {
			evidence.Earlier = append(evidence.Earlier, check.Run)
		}
	}
	evidence.extrasShown(added)
	evidence.outsideProfile(required, checks)
	evidence.settle()
	return evidence
}

// addBuilds gives the evidence a row for each build an earlier check of
// the files planned that it lacks, newest check first, for fill to fill
// as it fills the others: check-772's curl +gnutls +sectrust and
// check-773's curl +ssl +sectrust vanished once check-774 built curl with
// its defaults (field testing's note 10 from the curl fix). A port's rows
// go together, its default build first, then its variant builds as their
// checks come, newest first. A build beside a default build the evidence
// has is an extra, as an --also port is, which asks nothing where no
// check built it; one of a port with no default build there keeps its
// check's role, the port's coverage. The extras it added are returned.
func (e *Evidence) addBuilds(primary Check, earlier []Check) map[model.TargetID]bool {
	added := map[model.TargetID]bool{}
	for _, check := range earlier {
		if check.Run.ID == primary.Run.ID {
			continue
		}
		for _, target := range check.Plan.Targets {
			if slices.ContainsFunc(e.Targets, func(t TargetEvidence) bool { return t.Target.ID == target.ID }) {
				continue
			}
			name := target.Target.Name
			if slices.ContainsFunc(e.Targets, func(t TargetEvidence) bool {
				return t.Target.Target.Name == name && t.Target.Target.VariantSpec() == ""
			}) {
				target.Role = model.Also
				added[target.ID] = true
			}
			row := TargetEvidence{Target: target}
			for _, environment := range e.Plan.Environments {
				cell := noResult(CellNotRun, environment, target.ID)
				if exclusion, ok := check.Plan.ExclusionIn(environment, target.ID); ok {
					cell = noResult(CellExcluded, environment, target.ID)
					cell.Exclusion = exclusion
				}
				row.Outcomes = append(row.Outcomes, cell)
			}
			at := len(e.Targets)
			for i, t := range e.Targets {
				if t.Target.Target.Name == name {
					at = i + 1
				}
			}
			if target.Target.VariantSpec() == "" {
				if i := slices.IndexFunc(e.Targets, func(t TargetEvidence) bool { return t.Target.Target.Name == name }); i >= 0 {
					at = i
				}
			}
			e.Targets = slices.Insert(e.Targets, at, row)
		}
	}
	return added
}

// extrasShown shows a build addBuilds added beside a default build only
// where a check built it: elsewhere its cell is no check's to ask, as a
// release outside the newest check's ask reads, never unchecked.
func (e *Evidence) extrasShown(added map[model.TargetID]bool) {
	for t := range e.Targets {
		if !added[e.Targets[t].Target.ID] {
			continue
		}
		for i, cell := range e.Targets[t].Outcomes {
			if cell.Kind != CellNotRun && cell.Kind != CellRemade && cell.Kind != CellEntered {
				continue
			}
			skipped := noResult(CellExcluded, cell.Environment, e.Targets[t].Target.ID)
			skipped.Exclusion = model.Exclusion{Target: e.Targets[t].Target.Target, Reason: "outside what the newest check of these files asked for", Span: true}
			e.Targets[t].Outcomes[i] = skipped
		}
	}
}

// outsideProfile asks nothing of a target in an environment the newest
// check of the files didn't ask for (required, Covers): a cell no check
// built there, or whose result was recorded in an environment that is
// another now, reads "—", as a release a profile skipped does, and counts
// for nothing, so asking again replaces an earlier ask (D17, the person's
// "Newest ask", 2026-10-29). What a check found there still counts, a
// failure above all, which blocks or is accepted, so a narrower ask never
// un-requires one. Plan takes every check's environments, so one
// diagnostic check --only ufraw on macOS 13 and 14 had submit ask for all
// six of the branch's ports there (the exiv2 session's notes,
// 2026-10-21). Inside what was asked, a target a check planned and didn't
// reach, as when it stopped, names that check, as the block says where
// (the exiv2 session's note 7).
func (e *Evidence) outsideProfile(required []model.Environment, checks []Check) {
	planned := func(environment model.Environment, id model.TargetID) string {
		for _, check := range checks {
			if build, ok := check.Plan.In(environment); ok && build.Builds(id) {
				return check.Run.Name()
			}
		}
		return ""
	}
	for i, environment := range e.Plan.Environments {
		asked := slices.ContainsFunc(required, func(r model.Environment) bool { return Covers(r, environment) })
		for t := range e.Targets {
			cell := &e.Targets[t].Outcomes[i]
			switch {
			case asked && cell.Kind == CellNotRun && cell.Refused == nil:
				cell.PlannedIn = planned(environment, e.Targets[t].Target.ID)
			case !asked && (cell.Kind == CellNotRun || cell.Kind == CellRemade || cell.Kind == CellEntered):
				skipped := noResult(CellExcluded, environment, e.Targets[t].Target.ID)
				skipped.Exclusion = model.Exclusion{Target: e.Targets[t].Target.Target, Reason: "outside what the newest check of these files asked for", Span: true}
				*cell = skipped
			}
		}
	}
}

// Plan is the plan a tree's evidence is judged against: the primary
// check's, with each environment another check of the same files planned,
// by the newest that did (others, newest first), and the required ones,
// what the newest check asked for, which no check may have planned (D17).
// A check narrowed to one release replaced the others' results:
// sand-runner's check on macOS 15 alone left its pass on 26 out of status
// and submit (the sand-runner port). An environment no check planned has
// no plan of its own, and what it requires is unchecked there.
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

// CellCheck names the check a cell's result came from where it isn't the
// newest check's own: the check that built a reused result, or an earlier
// check of the same files whose result fills the cell (fill). Empty for
// the newest check's own, and for a cell with no result. The terminal
// says it beside the cell, as "✓ (check-440)": after an edit between
// checks, nothing said which cells were the newest's (the exiv2 session's
// notes, 2026-10-21).
func (e Evidence) CellCheck(cell Cell) string {
	if cell.Kind != CellRecorded {
		return ""
	}
	if cell.ReusedFrom != "" {
		if found, ok := e.origins[cell.ReusedFrom]; ok {
			return found.Check
		}
	}
	execution, ok := e.Executions[cell.Execution]
	if !ok || execution.Run == e.Run.ID {
		return ""
	}
	return e.Checks()[execution.Run]
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

// Took is how long the check's own provider runs in an environment took,
// from each run's start to its end, every attempt counted: its wall time
// there, which a person reads to see where a check spent it (the docs
// dig's I38). False where the check ran nothing there that finished, as
// where every result was an earlier check's.
func (e Evidence) Took(environment int) (time.Duration, bool) {
	var took time.Duration
	ran := false
	for _, run := range e.Runs(environment) {
		if run.Run != e.Run.ID || run.FinishedAt == nil {
			continue
		}
		took += run.FinishedAt.Sub(run.CreatedAt)
		ran = true
	}
	return took, ran
}

// TargetTook is how long a target's build took in an environment: from
// the result its run recorded before it, or the run's start, to its own,
// since a run builds its targets in order and records each as it ends.
// False for a result reused from an earlier build, one another check
// recorded, or one not recorded.
func (e Evidence) TargetTook(target TargetEvidence, environment int) (time.Duration, bool) {
	if environment >= len(target.Outcomes) {
		return 0, false
	}
	cell := target.Outcomes[environment]
	run, ok := e.Executions[cell.Execution]
	if cell.Kind != CellRecorded || cell.ReusedFrom != "" || !ok || run.Run != e.Run.ID || cell.RecordedAt.IsZero() {
		return 0, false
	}
	since := run.CreatedAt
	for _, other := range e.Targets {
		if environment >= len(other.Outcomes) {
			continue
		}
		earlier := other.Outcomes[environment]
		if earlier.Execution == cell.Execution && earlier.ReusedFrom == "" && earlier.RecordedAt.Before(cell.RecordedAt) && earlier.RecordedAt.After(since) {
			since = earlier.RecordedAt
		}
	}
	return cell.RecordedAt.Sub(since), true
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

// ProfileSkips says the build profile chose not to build any target in an
// environment, as minmax does past a port's oldest and newest releases
// (Exclusion.Span), where nothing about the ports excludes it.
func (e Evidence) ProfileSkips(environment int) bool {
	for _, target := range e.Targets {
		exclusion, excluded := e.Plan.ExclusionIn(e.Plan.Environments[environment], target.Target.ID)
		if !excluded || !exclusion.Span {
			return false
		}
	}
	return len(e.Targets) > 0
}

// Shown are the environments a result table shows, by index: each some
// target was built on, or meant to be. One every target was excluded from,
// whether a minmax profile chose not to build there or the port's own
// platforms leave it out, the table leaves out rather than filling with
// "not built" (the person's word, 2026-10-16: "Just keep the cells of the
// macOS versions that builds actually happened on"; field testing's tart
// #35590, at >= 24, still showed macOS 12 to 14).
func (e Evidence) Shown() []int {
	var shown []int
	for i := range e.Plan.Environments {
		for _, target := range e.Targets {
			if target.Outcomes[i].Kind != CellExcluded {
				shown = append(shown, i)
				break
			}
		}
	}
	if len(shown) == 0 {
		for i := range e.Plan.Environments {
			shown = append(shown, i)
		}
	}
	return shown
}

// Checked are the environments of Shown that a check of the files
// planned, by index, leaving out those build.default's profile requires
// and no check planned (Plan), whose cells are all not run: status showed
// "macOS 26 · not run" for a branch only ever checked on 12 and 27, from
// a Mac on 26 (field testing's exiv2, 2026-10-20). They're what's left to
// check, which submit says, not a result: a check's report and status
// show Checked, and the pull request Shown. Where no check planned any, it
// is Shown.
func (e Evidence) Checked() []int {
	var checked []int
	for _, i := range e.Shown() {
		if slices.ContainsFunc(e.Plan.Builds, func(build model.EnvironmentPlan) bool { return build.Environment == e.Plan.Environments[i] }) {
			checked = append(checked, i)
		}
	}
	if len(checked) == 0 {
		return e.Shown()
	}
	return checked
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

// Unfinished reports evidence that another check would settle: some
// target asks for a check, and none that didn't pass has a result that
// failed. One with a failure stands for these files: checking them again
// reuses it.
func (e Evidence) Unfinished() bool {
	failed := e.Failed()
	return len(failed) > 0 && !slices.ContainsFunc(failed, func(t TargetEvidence) bool {
		return !t.Missing() || slices.ContainsFunc(t.Outcomes, func(c Cell) bool { return c.Kind == CellRecorded && c.Outcome != model.OutcomePassed })
	})
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
			if c.Kind == CellNotRun || c.Kind == CellRemade || c.Kind == CellEntered {
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
	return countsRefusal(recorded, execution, id, now, git, fetched, other) == nil
}

// countsRefusal is Counts' rule, saying which part of it a result fails:
// nil where it counts. A dependency's kind is said without the dependency,
// which the caller knows (readSource).
func countsRefusal(recorded model.Plan, execution model.GuestExecution, id model.TargetID, now string, git *model.GitSource, fetched model.ObjectID, other bool) *Refusal {
	planned, ok := recorded.In(execution.Environment)
	switch {
	case !ok || !planned.Builds(id):
		return &Refusal{Kind: RefusedNotPlanned}
	case !current(execution, now):
		return &Refusal{Kind: refusedRemade}
	case git != nil && execution.ID != "":
		switch git.AgainstFetched(fetched) {
		case model.SourceSame:
		case model.SourceMoved:
			return &Refusal{Kind: RefusedMoved, Ref: git.Ref, Then: string(fetched), Now: git.Expected()}
		default:
			if fetched == "" && git.Expected() != "" {
				return &Refusal{Kind: RefusedUnrecorded, Ref: git.Ref, Now: git.Expected()}
			}
			// The reading judged against couldn't be resolved: the result's
			// own check's, where it couldn't either, else the newest's.
			if own, ok := recorded.GitIn(execution.Environment, id); ok && own.Expected() == "" {
				return &Refusal{Kind: RefusedUnread, Ref: git.Ref}
			}
			return &Refusal{Kind: RefusedUnreadNow, Ref: git.Ref, Then: string(fetched)}
		}
	}
	if other {
		return &Refusal{Kind: RefusedDependency}
	}
	if own, fetchedWithGit := recorded.GitIn(execution.Environment, id); fetchedWithGit && own.AgainstFetched(fetched) == model.SourceMoved {
		return &Refusal{Kind: RefusedOwnMove, Ref: own.Ref, Then: own.Expected(), Now: string(fetched)}
	}
	return nil
}

// newestReadings is the merged plan with each environment the newest
// check didn't plan given the newest check's own resolution of each
// target's Git source and tag, where the newest resolved it: one freshly
// made plan reads each repository and ref once, so any of its
// environments says what it read. Where the newest couldn't resolve it,
// the earlier environment keeps its own reading, and only a shown move
// unseats anything (change 4).
func newestReadings(newest, merged model.Plan) model.Plan {
	readings := merged
	readings.Builds = slices.Clone(merged.Builds)
	reading := func(sources func(model.EnvironmentPlan) map[model.TargetID]model.GitSource, id model.TargetID, url, ref string) (model.GitSource, bool) {
		for _, build := range newest.Builds {
			if source, ok := sources(build)[id]; ok && source.URL == url && source.Ref == ref && source.Expected() != "" {
				return source, true
			}
		}
		return model.GitSource{}, false
	}
	for i, build := range readings.Builds {
		if slices.Contains(newest.Environments, build.Environment) {
			continue
		}
		for _, kind := range []struct {
			get func(model.EnvironmentPlan) map[model.TargetID]model.GitSource
			set func(*model.EnvironmentPlan, map[model.TargetID]model.GitSource)
		}{
			{func(b model.EnvironmentPlan) map[model.TargetID]model.GitSource { return b.Git }, func(b *model.EnvironmentPlan, m map[model.TargetID]model.GitSource) { b.Git = m }},
			{func(b model.EnvironmentPlan) map[model.TargetID]model.GitSource { return b.Tags }, func(b *model.EnvironmentPlan, m map[model.TargetID]model.GitSource) { b.Tags = m }},
		} {
			sources := kind.get(build)
			if len(sources) == 0 {
				continue
			}
			replaced := maps.Clone(sources)
			for id, source := range sources {
				if newer, ok := reading(kind.get, id, source.URL, source.Ref); ok {
					replaced[id] = newer
				}
			}
			kind.set(&readings.Builds[i], replaced)
		}
	}
	return readings
}

// shownMoves are the targets whose tag any earlier check's plan shows
// moved against the newest reading of it, in any environment: the source
// comparison design's "unless another build of the target shows the tag
// moved", which one earlier check judged alone forgot when it tried the
// next (Codex's review of #192, finding 1).
func shownMoves(checks []Check, readings model.Plan) map[model.TargetID]bool {
	shown := map[model.TargetID]bool{}
	for _, check := range checks {
		for _, build := range check.Plan.Builds {
			for id := range build.Tags {
				if model.SourceAgainst(check.Plan.TagIn(build.Environment, id), readings.TagIn(build.Environment, id)) == model.SourceMoved {
					shown[id] = true
				}
			}
		}
	}
	return shown
}

// sourceRefusal says whether an earlier check's plan fetched a target the
// way the newest plan does, where the newest builds it there, and if not,
// how not; nil where the result stands. A tag that named another commit
// then than now unseats the result, as reuse builds again for it (change
// 2), and a result built with Git counts for no plan that builds the
// target another way (change 3). An archive's tag that wasn't known on one
// side stands, the Portfile's checksums saying what was built, unless
// another build of the target shows the tag moved (shownMoved); another
// release's tag stands. A target the newest plan doesn't build there is
// judged against its own plan alone (Counts).
func sourceRefusal(earlier, newest model.Plan, environment model.Environment, id model.TargetID, built, shownMoved bool) *Refusal {
	if !built {
		return nil
	}
	then, now := earlier.TagIn(environment, id), newest.TagIn(environment, id)
	switch model.SourceAgainst(then, now) {
	case model.SourceMoved:
		return &Refusal{Kind: RefusedMoved, Ref: now.Ref, Then: then.Expected(), Now: now.Expected()}
	case model.SourceUnknown:
		switch {
		case !shownMoved:
		case then.Expected() == "":
			return &Refusal{Kind: RefusedUnread, Ref: then.Ref}
		default:
			// Its tag isn't known now, and another check shows it moved.
			return &Refusal{Kind: RefusedMoved, Ref: then.Ref, Then: then.Expected()}
		}
	}
	_, earlierGit := earlier.GitIn(environment, id)
	_, newestGit := newest.GitIn(environment, id)
	if earlierGit && !newestGit {
		return &Refusal{Kind: RefusedFetchMode}
	}
	return nil
}

// UnreadIdentities are the environments whose results the evidence rests
// on and whose identity couldn't be read now, each with the cause: their
// results are taken as they were, never marked remade, since whether the
// environment is the one they were built in isn't known (#190).
func (e Evidence) UnreadIdentities() map[model.Environment]string {
	unread := map[model.Environment]string{}
	for i, environment := range e.Plan.Environments {
		identity, ok := e.now[environment]
		if !ok || identity.Unread == "" {
			continue
		}
		for _, target := range e.Targets {
			if i < len(target.Outcomes) && target.Outcomes[i].Kind == CellRecorded && target.Outcomes[i].Execution != "" {
				unread[environment] = identity.Unread
				break
			}
		}
	}
	return unread
}

// Fetched is the commit a recorded cell's build said it fetched with Git,
// empty where it said none or fetched no other way.
func (e Evidence) Fetched(c Cell) model.ObjectID { return e.inputs[c.Inputs].Fetched }

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
			if !ok || current(execution, e.now[execution.Environment].Value) {
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
	// The earlier check's own Git builds are read too, where the newest
	// plan fetches nothing with Git, so a result is judged against its own
	// plan with what it fetched (Codex's review of #192, finding 3).
	if !ReadsSources(primary) && !ReadsSources(e.Plan) {
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
			// The tag the earlier check planned beside its build, and the
			// newest plan's, so a dependent built against a moved tag's old
			// archive is found here as reuse finds it (change 2 of
			// docs/designs/source-comparison.md).
			build := reuse.Candidate{Result: c.TargetResult, Inputs: inputs, Tagged: e.Plan.TagIn(environment, target.Target.ID)}
			planned := reuse.Target{PlanTarget: target.Target, DependsOn: e.Plan.DependsOnIn(environment, target.Target.ID), Earlier: []reuse.Candidate{build},
				Tag: primary.TagIn(environment, target.Target.ID), TagShownMoved: e.shown[target.Target.ID]}
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
		for id, against := range reuse.AgainstOtherSources(targets, built) {
			read := e.sources[keys[id]]
			read.against = &against
			e.sources[keys[id]] = read
		}
	}
}

// refuse marks a cell not run with why an earlier check's result there
// doesn't count: the first check's fill tried, newest first, that had one.
// A dependency's kind is given the dependency it rested on and how that
// dependency's source reads then and now (readings).
func (earlier Evidence) refuse(cell *Cell, why Refusal, against *reuse.Against, readings model.Plan) {
	if cell.Kind != CellNotRun || cell.Refused != nil {
		return
	}
	why.Check = earlier.Run.Name()
	if why.Kind == RefusedDependency && against != nil {
		why.Dependency = against.Dependency
		then, now := sourceReading(earlier.Plan, cell.Environment, against.Dependency), sourceReading(readings, cell.Environment, against.Dependency)
		switch {
		case against.Indirect:
			why.Kind = RefusedDependencyRefused
		case model.SourceAgainst(then, now) == model.SourceMoved:
			why.Ref, why.Then, why.Now = now.Ref, then.Expected(), now.Expected()
		default:
			why.Kind = RefusedDependencyUnshown
			if now != nil {
				why.Ref, why.Now = now.Ref, now.Expected()
			}
		}
	}
	cell.Refused = &why
}

// sourceReading is how a plan reads a target's source in an environment:
// its Git source, else its GitHub tag; nil where it has neither.
func sourceReading(plan model.Plan, environment model.Environment, id model.TargetID) *model.GitSource {
	if source, ok := plan.GitIn(environment, id); ok {
		return &source
	}
	return plan.TagIn(environment, id)
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
			if result.Kind != CellNotRun && result.Kind != CellRemade && result.Kind != CellEntered {
				continue
			}
			j := slices.Index(earlier.Plan.Environments, environment)
			if j < 0 {
				continue
			}
			cell := earlier.Targets[k].Outcomes[j]
			if cell.Kind != CellRecorded && cell.Kind != CellUnmet {
				continue
			}
			execution, recorded := earlier.Executions[cell.Execution]
			if !recorded {
				// An unmet result is the plan's, with no execution behind it.
				execution = model.GuestExecution{Environment: environment}
			}
			readings := e.Plan
			if earlier.readings.Builds != nil {
				readings = earlier.readings
			}
			var git *model.GitSource
			if source, ok := readings.GitIn(environment, target.Target.ID); ok {
				git = &source
			}
			read := earlier.sources[resultKey(cell.TargetResult)]
			newest, builds := e.Plan.In(environment)
			why := countsRefusal(earlier.Plan, execution, target.Target.ID, e.now[environment].Value, git, read.fetched, read.against != nil)
			if why == nil && cell.Kind == CellRecorded {
				why = sourceRefusal(earlier.Plan, readings, environment, target.Target.ID, builds && newest.Builds(target.Target.ID), earlier.shown[target.Target.ID])
			}
			if why != nil {
				if cell.Kind == CellRecorded && !current(execution, e.now[environment].Value) {
					target.Outcomes[i].Kind, target.Outcomes[i].Recorded = CellRemade, execution.Identity
				} else {
					earlier.refuse(&target.Outcomes[i], *why, read.against, readings)
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
	blockers := earlier.blockers()
	standing(stands, blockers)
	took := false
	for _, p := range found {
		if key := resultKey(p.cell.TargetResult); p.cell.Kind == CellRecorded && !stands[key] {
			// A blocked result goes with the blocker's that doesn't stand.
			if i := slices.IndexFunc(blockers[key], func(by [2]string) bool { return !stands[by] }); i >= 0 {
				earlier.refuse(&e.Targets[p.t].Outcomes[p.i], Refusal{Kind: RefusedBlocked, Dependency: model.TargetID(blockers[key][i][1])}, nil, e.Plan)
			}
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
		// What the build read comes with its result, so a later question
		// of it, what it fetched with Git (Fetched), is answered.
		if inputs, ok := earlier.inputs[p.cell.Inputs]; ok && p.cell.Inputs != "" {
			if e.inputs == nil {
				e.inputs = map[string]model.TargetInputs{}
			}
			if _, have := e.inputs[p.cell.Inputs]; !have {
				e.inputs[p.cell.Inputs] = inputs
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

// VariantBuild is a variant build of a port the branch changes, as the
// evidence holds it: its port, its variants as MacPorts writes them, the
// checks whose results its cells are, and, where it failed and that
// stands, the checks it failed in.
type VariantBuild struct {
	Port, Spec string
	Checks     []string
	Failed     []string
}

// VariantBuilds are the evidence's variant builds of the ports the branch
// changes, from any check of the files, --variants each's and chosen
// variants' alike, in the evidence's order: what the pull request
// template's variants item asks, which they answer where every one passed
// (field testing's note 10 from the curl fix, where check-772's and
// check-773's builds went unsaid beside check-774's default build).
func (e Evidence) VariantBuilds() []VariantBuild {
	names := e.Checks()
	changed := map[string]bool{}
	for _, target := range e.Targets {
		if target.Target.Role == model.Changed {
			changed[target.Target.Target.Name] = true
		}
	}
	var builds []VariantBuild
	for _, target := range e.Targets {
		spec := target.Target.Target.VariantSpec()
		if spec == "" || !changed[target.Target.Target.Name] {
			continue
		}
		build := VariantBuild{Port: target.Target.Target.Name, Spec: spec}
		for _, cell := range target.Outcomes {
			if cell.Kind != CellRecorded {
				continue
			}
			run := e.Executions[cell.Execution].Run
			if found, ok := e.origins[cell.ReusedFrom]; ok {
				run = found.Execution.Run
			}
			name := names[run]
			if name == "" {
				continue
			}
			if !slices.Contains(build.Checks, name) {
				build.Checks = append(build.Checks, name)
			}
			if cell.Outcome != model.OutcomePassed && !slices.Contains(build.Failed, name) {
				build.Failed = append(build.Failed, name)
			}
		}
		if len(build.Checks) > 0 {
			builds = append(builds, build)
		}
	}
	return builds
}

// VariantsAnswered reports whether the evidence's variant builds answer
// the template's variants item: there's one, and every one passed
// wherever it ran.
func (e Evidence) VariantsAnswered() bool {
	builds := e.VariantBuilds()
	return len(builds) > 0 && !slices.ContainsFunc(builds, func(b VariantBuild) bool { return len(b.Failed) > 0 })
}

// Ask is the governing ask of a branch's checks (D17): the newest that
// covers every changed port, a check narrowed with --only adding its
// results but setting none. Where it was asked, by --on or --build, and
// by which profile, and its environments, are what submission requires
// and whose the branch is to submit: a full check --on local, then a
// default check narrowed to one port, left the person's environments
// required and made the branch serve's to submit (Codex's review of
// 275125ff, finding 1).
type Ask struct {
	Asked        bool
	Profile      string
	Environments []model.Environment
}

// AskOf is the governing ask among a tree's checks' plans, newest first;
// nil where every one was narrowed.
func AskOf(plans []model.Plan) *Ask {
	for _, plan := range plans {
		if len(plan.Omitted) == 0 {
			return &Ask{Asked: plan.Asked, Profile: plan.Profile, Environments: slices.Clone(plan.Environments)}
		}
	}
	return nil
}
