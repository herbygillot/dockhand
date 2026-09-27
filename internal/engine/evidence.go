package engine

import (
	"context"
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/buildenv"
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
	// environment it is required in: --only left it out, the check stopped
	// before it, or the environment has been made again since.
	Unchecked bool
	// Remade are the environments where the target's result was recorded
	// before the environment was made again, from another source or with
	// other tools: another environment, whose results don't stand for it
	// (Counts).
	Remade []model.Environment
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
// runs: a run that reused earlier builds is shown by those builds, the runs
// a reviewer can look at, each with the check that reused it (reusedIn).
func (e Evidence) Built(environment int, runs []model.GuestExecution) (built []model.GuestExecution, reusedIn map[model.ExecutionID]string) {
	reusedIn = map[model.ExecutionID]string{}
	add := func(run model.GuestExecution) {
		if !slices.ContainsFunc(built, func(other model.GuestExecution) bool { return other.ID == run.ID }) {
			built = append(built, run)
		}
	}
	for _, run := range runs {
		if !run.Reused {
			add(run)
			continue
		}
		for _, target := range e.Targets {
			if environment >= len(target.Outcomes) {
				continue
			}
			result := target.Outcomes[environment]
			found, ok := e.origins[result.ReusedFrom]
			if result.Execution != run.ID || !ok {
				continue
			}
			add(found.execution)
			reusedIn[found.execution.ID] = e.Checks()[run.Run]
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
	return targetWords(e.Plan, target.Target, e.Plan.Environments[environment], result, e.testsReading(result), accepted)
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
		provider, ok := e.Providers[environment.Provider].(buildenv.IdentityProvider)
		if !ok {
			continue
		}
		if identity, err := provider.Identity(ctx, environment); err == nil {
			now[environment] = identity
		}
	}
	return now
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
	return evidence, err
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
// quietly shrinks what submit requires). A result recorded in an
// environment that is another now, by its identity, doesn't count (Counts).
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
			if result.Outcome == model.OutcomeNotRun && !Excluded(e.Plan, target.Target, e.Plan.Environments[i]) {
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
//     source, or with other tools, is another environment.
//
// Nothing else about the check's selection matters: from the same files,
// a target builds the same whichever ports were selected with it. Nor does
// its test policy: a result keeps the policy of the check that recorded it,
// and reads under it (decision D1, Evidence.Words).
func Counts(recorded model.Plan, execution model.GuestExecution, id model.TargetID, now string) bool {
	planned, ok := recorded.In(execution.Environment)
	return ok && planned.Builds(id) && current(execution, now)
}

// current reports whether an execution ran in the environment there is
// now, by its identity. One whose provider can't say what it is now is
// taken as it was, and so is a result no execution recorded, such as an
// unmet need, which is the plan's.
func current(execution model.GuestExecution, now string) bool {
	return execution.ID == "" || now == "" || execution.Identity == now
}

// dropRemade drops the results recorded in an environment that is another
// now, by its identity, and notes where: they are the environment's as it
// was, which no longer stands for it.
func (e *Evidence) dropRemade() {
	for t := range e.Targets {
		target := &e.Targets[t]
		for i, result := range target.Outcomes {
			execution, ok := e.Executions[result.Execution]
			if !ok || current(execution, e.now[execution.Environment]) {
				continue
			}
			target.Outcomes[i] = model.TargetResult{Target: result.Target, Outcome: model.OutcomeNotRun}
			target.remade(execution.Environment)
		}
	}
}

// remade notes an environment where the target's result was recorded
// before the environment was made again.
func (t *TargetEvidence) remade(environment model.Environment) {
	if !slices.Contains(t.Remade, environment) {
		t.Remade = append(t.Remade, environment)
	}
}

// fill takes an earlier check's results for what this evidence lacks,
// where they count (Counts), and reports whether it took any.
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
			if result.Outcome != model.OutcomeNotRun || Excluded(e.Plan, target.Target, environment) {
				continue
			}
			j := slices.Index(earlier.Plan.Environments, environment)
			if j < 0 {
				continue
			}
			found := earlier.Targets[k].Outcomes[j]
			execution, recorded := earlier.Executions[found.Execution]
			if !recorded {
				// An unmet result is the plan's, with no execution behind it.
				execution = model.GuestExecution{Environment: environment}
			}
			if found.Outcome == model.OutcomeNotRun || !Counts(earlier.Plan, execution, target.Target.ID, e.now[environment]) {
				if found.Outcome != model.OutcomeNotRun && !current(execution, e.now[environment]) {
					target.remade(environment)
				}
				continue
			}
			target.Outcomes[i] = found
			if builder, ok := earlier.origins[found.ReusedFrom]; ok {
				if e.origins == nil {
					e.origins = map[model.ExecutionID]origin{}
				}
				e.origins[found.ReusedFrom] = builder
			}
			if recorded {
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
	}
	return took
}

// settle works out each target's verdict from its outcomes: passed where
// it passed in every environment it is required in, and unchecked where
// one has no result. Remade keeps only the environments still without
// one, since an earlier check may have built it there as it is now.
func (e *Evidence) settle() {
	for t := range e.Targets {
		target := &e.Targets[t]
		target.Remade = slices.DeleteFunc(target.Remade, func(environment model.Environment) bool {
			i := slices.Index(e.Plan.Environments, environment)
			return i < 0 || target.Outcomes[i].Outcome != model.OutcomeNotRun
		})
		target.Passed, target.Unchecked = true, false
		for i, result := range target.Outcomes {
			if Excluded(e.Plan, target.Target, e.Plan.Environments[i]) {
				continue
			}
			target.Passed = target.Passed && result.Outcome == model.OutcomePassed
			target.Unchecked = target.Unchecked || result.Outcome == model.OutcomeNotRun || result.Outcome == model.OutcomeUnmet
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
		unmet, needs := evidence.unmet(target)
		switch {
		case needs && target.Target.Role != model.Also:
			problems = append(problems, fmt.Sprintf("%s %s, which %s hasn't; a check with %s there builds it, or share the branch as a draft (--draft)",
				name, UnmetWords(unmet), DescribeEnvironment(unmet.Environment), unmet.Needs))
		case target.Unchecked && len(target.Remade) > 0 && target.Target.Role != model.Also:
			problems = append(problems, fmt.Sprintf("%s was checked in %s before it was made again, from another source or with other tools; dockhand check builds it there again, or share the branch as a draft (--draft)", name, DescribeEnvironment(target.Remade[0])))
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

// unmet is the first environment that can't build a target, when one
// can't, and it has no result from an earlier check there either.
func (e Evidence) unmet(target TargetEvidence) (model.Unmet, bool) {
	for i, result := range target.Outcomes {
		if result.Outcome == model.OutcomeUnmet {
			return e.Plan.UnmetIn(e.Plan.Environments[i], target.Target.ID)
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

// Excluded reports whether the plan leaves a target out in an environment,
// where it is not built and not required to pass.
func Excluded(plan model.Plan, target model.PlanTarget, environment model.Environment) bool {
	return plan.Excludes(target, environment)
}
