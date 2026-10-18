package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
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
)

const (
	CellRecorded = evidence.CellRecorded
	CellExcluded = evidence.CellExcluded
	CellUnmet    = evidence.CellUnmet
	CellNotRun   = evidence.CellNotRun
	CellRemade   = evidence.CellRemade
)

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
			return "on " + DescribeEnvironment(c.Environment) + ", " + c.Change
		}
		return DescribeEnvironment(c.Environment) + " was made again, from another source or with other tools"
	}
	return ""
}

// EvidenceWords is how one target's result in one environment reads, on the
// terminal and in the pull request alike (TargetWords). A result's tests
// read under the policy of the check that built it: when that check is an
// earlier one whose policy differs from this evidence's own, it is named,
// "tests failed (advisory, check-3)", so a later check with --tests
// required never makes an earlier advisory result read as required.
func EvidenceWords(evidence Evidence, target TargetEvidence, environment int, accepted string) string {
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

// identitiesNow asks each environment's provider what it is now. It is
// asked outside any transaction, since a transaction never calls a
// provider; one that can't say leaves the environment's identity unknown.
func (e *Engine) identitiesNow(ctx context.Context, environments []model.Environment) evidence.Identities {
	now := evidence.Identities{}
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

// evidenceNow is what a tree's checks establish now (evidence.Judge): the
// plan they're judged against, then the environments' identities, read
// outside any transaction, then the checks' records.
func (e *Engine) evidenceNow(ctx context.Context, primary model.Run, runs []model.Run) (Evidence, error) {
	required, problem := e.requiredEnvironments(ctx)
	var plan model.Plan
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		own, err := r.Plan(primary.Plan)
		if err != nil {
			return err
		}
		var others []model.Plan
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
		plan = evidence.Plan(own, others, required)
		return nil
	}); err != nil {
		return Evidence{}, err
	}
	now := e.identitiesNow(ctx, plan.Environments)
	var judged Evidence
	err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		own, err := loadCheck(r, primary, false)
		if err != nil {
			return err
		}
		var earlier []evidence.Check
		for _, run := range runs {
			if run.ID == primary.ID {
				continue
			}
			check, err := loadCheck(r, run, evidence.ReadsSources(plan))
			if err != nil {
				return err
			}
			earlier = append(earlier, check)
		}
		judged = evidence.Judge(own, plan, earlier, now)
		judged.Problem = problem
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

// loadCheck loads a finished check's record, as evidence reads it: its
// plan, its executions and their results, the executions that built the
// results it reused, with their checks, and, where sources asks, what its
// results' builds read.
func loadCheck(r store.Reader, run model.Run, sources bool) (evidence.Check, error) {
	plan, err := r.Plan(run.Plan)
	if err != nil {
		return evidence.Check{}, err
	}
	return loadCheckWith(r, run, plan, sources)
}

// loadCheckWith is loadCheck with the check's plan already read.
func loadCheckWith(r store.Reader, run model.Run, plan model.Plan, sources bool) (evidence.Check, error) {
	check := evidence.Check{Run: run, Plan: plan, Results: map[model.ExecutionID][]model.TargetResult{}}
	executions, err := r.Executions(run.ID)
	if err != nil {
		return evidence.Check{}, err
	}
	check.Executions = executions
	for _, execution := range executions {
		results, err := r.Results(execution.ID)
		if err != nil {
			return evidence.Check{}, err
		}
		check.Results[execution.ID] = results
		for _, result := range results {
			if result.ReusedFrom != "" {
				if _, ok := check.Origins[result.ReusedFrom]; !ok {
					builder, err := r.Execution(result.ReusedFrom)
					if err != nil {
						return evidence.Check{}, err
					}
					builtIn, err := r.Run(builder.Run)
					if err != nil {
						return evidence.Check{}, err
					}
					if check.Origins == nil {
						check.Origins = map[model.ExecutionID]evidence.Origin{}
					}
					check.Origins[result.ReusedFrom] = evidence.Origin{Execution: builder, Check: builtIn.Name()}
				}
			}
			if sources && result.Inputs != "" {
				if _, ok := check.Inputs[result.Inputs]; !ok {
					inputs, err := r.Inputs(result.Inputs)
					if err != nil {
						return evidence.Check{}, err
					}
					if check.Inputs == nil {
						check.Inputs = map[string]model.TargetInputs{}
					}
					check.Inputs[result.Inputs] = inputs
				}
			}
		}
	}
	return check, nil
}

// treeRuns lists a branch's finished checks of a tree, newest first. A
// baseline is evidence about another run, never the branch's own check.
func treeRuns(r store.Reader, branch model.BranchID, tree model.ObjectID) ([]model.Run, error) {
	return runsOfTree(r, branch, tree, model.RunPassed, model.RunFailed, model.RunAttention)
}

// runsOfTree are a branch's checks of a tree in the given states, newest
// first, baselines aside.
func runsOfTree(r store.Reader, branch model.BranchID, tree model.ObjectID, states ...model.RunState) ([]model.Run, error) {
	return r.Runs(store.RunFilter{Branch: branch, Tree: tree, States: states, NoBaselines: true})
}

// requiredEnvironments are the environments build.default's profile builds
// in, which a branch's evidence always requires (D17); none where they
// can't be resolved, as where a provider isn't set up, with why, for the
// evidence to say, where it was dropped and the evidence required less
// without a word (the architecture review's L5).
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

// acceptanceProblem is why a port submit --accept names can't be
// accepted: it wasn't checked, it passed, no check of these files built
// it, or it's a changed port, whose failure is shared as a draft. A port
// may have several builds, its variant builds beside its default one: the
// one to accept is one that failed.
func acceptanceProblem(evidence Evidence, accept []string, atBase map[string][]string) error {
	for _, port := range accept {
		i := slices.IndexFunc(evidence.Targets, func(t TargetEvidence) bool { return t.Target.Target.Name == port && t.Failing() })
		if i < 0 {
			i = slices.IndexFunc(evidence.Targets, func(t TargetEvidence) bool { return t.Target.Target.Name == port })
		}
		switch {
		case i < 0:
			return fmt.Errorf("--accept %s: %s checked no port %s", port, evidence.Run.Name(), port)
		case evidence.Targets[i].Passed:
			return fmt.Errorf("--accept %s: it passed in %s; there is nothing to accept", port, evidence.Run.Name())
		case evidence.Targets[i].Missing() || !evidence.Targets[i].Failing():
			return fmt.Errorf("--accept %s: no check of these files built it, so there is no failure to accept; dockhand check builds it", port)
		case !evidence.Targets[i].Acceptable() && len(atBase[port]) == 0:
			return fmt.Errorf("--accept %s: %s has changed. Its failure can be accepted only if the baseline fails the same way. Run dockhand check --baseline to compare, or use --draft to share the failing change", port, port)
		}
	}
	return nil
}

// publicationProblems applies the publication rule to evidence: every
// changed target checked and every substantive one passed, and every
// other failure accepted.
func publicationProblems(evidence Evidence, accepted []string, atBase map[string][]string) []string {
	blocked := blockedByAccepted(evidence, accepted)
	var problems []string
	for _, target := range evidence.Failed() {
		name := target.Target.Target.Name
		if slices.Contains(blocked, name) {
			continue
		}
		unmet, needs := target.Unmet()
		remade := target.Remade()
		switch {
		case needs && target.Missing():
			problems = append(problems, fmt.Sprintf("%s %s, which %s hasn't; a check with %s there builds it, or share the branch as a draft (--draft)",
				name, UnmetWords(unmet), DescribeEnvironment(unmet.Environment), unmet.Needs))
		case len(remade) > 0 && target.Missing():
			problems = append(problems, fmt.Sprintf("%s: the previous check is no longer valid because %s. Run dockhand check to rebuild in the affected environments, or share the branch with --draft", name, RemadeWords(target)))
		case target.Missing():
			problems = append(problems, fmt.Sprintf("%s has changed, but these files have not been checked in every required environment. Run dockhand check, or share the branch with --draft", name))
		case !target.Acceptable() && len(atBase[name]) > 0 && !slices.Contains(accepted, name):
			// A failure master has too is no regression, the usual case for
			// a MacPorts pull request: a person accepts it, never bump or
			// serve, and the pull request says so (pgdog 0.1.62 on macOS 12,
			// field testing; the person's word, 2026-10-17).
			problems = append(problems, fmt.Sprintf("%s failed in %s and has the same failure at the base (%s). Use --accept %s to submit with this existing failure disclosed, fix the failure, or share the branch with --draft", name, evidence.Run.Name(), strings.Join(atBase[name], ", "), name))
		case !target.Acceptable() && len(atBase[name]) == 0:
			problems = append(problems, fmt.Sprintf("%s did not pass in %s; fix it, or share it as a draft (--draft); dockhand check --baseline sees whether master fails it the same way", name, evidence.Run.Name()))
		case !slices.Contains(accepted, name):
			problems = append(problems, fmt.Sprintf("%s (%s) did not pass in %s; acknowledge it with --accept %s if its failure is not this branch's doing", name, kindWords(target.Target), evidence.Run.Name(), name))
		}
	}
	return problems
}

// blockedByAccepted are the ports that failed only as blocked, in each
// environment by failed dependencies all of which are accepted, or blocked
// so in turn: accepted with them, unnamed. py-textual-fastdatatable's stub,
// blocked by its five py3NN subports whose failures the base has too, was
// refused under --accept of the five, and as a changed port of its own
// (field testing at 56e328ea).
func blockedByAccepted(evidence Evidence, accepted []string) []string {
	ok := map[model.TargetID]bool{}
	for _, target := range evidence.Targets {
		if slices.Contains(accepted, target.Target.Target.Name) {
			ok[target.Target.ID] = true
		}
	}
	var found []string
	for changed := true; changed; {
		changed = false
		for _, target := range evidence.Failed() {
			if ok[target.Target.ID] || target.Missing() {
				continue
			}
			blocked, clear := false, true
			for n, cell := range target.Outcomes {
				// A release the profile or the Portfile left out is no
				// result, though its cell reads not run: check-416's stub
				// was refused for minmax's skipped releases (field testing
				// at 3cf14522).
				if cell.Kind == CellExcluded || cell.Outcome == model.OutcomePassed {
					continue
				}
				if cell.Outcome != model.OutcomeBlocked {
					clear = false
				}
				if !clear {
					break
				}
				build, _ := evidence.Plan.In(evidence.Plan.Environments[n])
				for _, dependency := range build.Dependencies[target.Target.ID] {
					if !ok[dependency] && dependencyFailed(evidence, dependency, n) {
						clear = false
					}
				}
				blocked = true
			}
			if blocked && clear {
				ok[target.Target.ID] = true
				found = append(found, target.Target.Target.Name)
				changed = true
			}
		}
	}
	return found
}

// dependencyFailed reports whether a target didn't pass in an environment
// it wasn't left out of.
func dependencyFailed(evidence Evidence, id model.TargetID, environment int) bool {
	for _, target := range evidence.Targets {
		if target.Target.ID == id {
			cell := target.Outcomes[environment]
			return cell.Kind != CellExcluded && cell.Outcome != model.OutcomePassed
		}
	}
	return false
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
	if err := e.Store.View(ctx, e.Repository, func(r store.Reader) error {
		var err error
		runs, err = r.Runs(store.RunFilter{Branch: branch, States: []model.RunState{model.RunPassed, model.RunFailed}})
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

func kindWords(target model.PlanTarget) string {
	if target.Role == model.Also {
		return "an extra from --also"
	}
	if target.Kind == model.RevisionOnly {
		return "revision bump only"
	}
	return "changed"
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
	return judged.Run, coversPlan(judged.Plan.Environments, judged.Plan.Tests, func(id model.TargetID, environment int) (Cell, bool) {
		for _, target := range judged.Targets {
			if target.Target.ID == id {
				return target.Outcomes[environment], true
			}
		}
		return Cell{}, false
	}, want), nil
}

// PlanCovers reports whether a check still to finish, have, would stand
// for want once it passed: every environment want builds in, every target
// want builds, and a test policy at least as strict. A rerun follows such
// a check, and queues another for one that doesn't.
func PlanCovers(have, want model.Plan) bool {
	if testsRank(have.Tests) < testsRank(want.Tests) || have.Fresh != want.Fresh || have.Variants != want.Variants || have.EachVariant != want.EachVariant {
		return false
	}
	for _, environment := range want.Environments {
		if !slices.Contains(have.Environments, environment) {
			return false
		}
	}
	for _, target := range want.Targets {
		if !slices.ContainsFunc(have.Targets, func(other model.PlanTarget) bool { return other.ID == target.ID }) {
			return false
		}
	}
	return true
}

// coversPlan is Covered's judgment: each of want's targets, where want
// builds it, a cell of the evidence that passed and stands under want's
// tests, or one the environment can't build (unmet) either way.
func coversPlan(environments []model.Environment, tests model.TestPolicy, cell func(model.TargetID, int) (Cell, bool), want model.Plan) bool {
	if testsRank(tests) < testsRank(want.Tests) {
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
		}
	}
	return true
}

// testsRank orders test policies by how much a pass under them shows:
// skipped tests, then declared ones reported, then required ones.
func testsRank(policy model.TestPolicy) int {
	switch policy {
	case model.TestsRequired:
		return 2
	case model.TestsDeclared:
		return 1
	}
	return 0
}
