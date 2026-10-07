package engine

import (
	"context"
	"fmt"
	"slices"

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
	Observation    = evidence.Observation
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
func EvidenceWords(evidence Evidence, target TargetEvidence, environment int, accepted bool) string {
	result := target.Outcomes[environment]
	return targetWords(evidence.Plan, target.Target, result, evidence.TestsReading(result.TargetResult), accepted)
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

// requiredEnvironments are the environments a check builds in by default,
// check.on's, which a branch's evidence always requires (D17); none where
// they can't be resolved, as where a provider isn't set up, with why, for
// the evidence to say, where it was dropped and the evidence required
// less without a word (the architecture review's L5).
func (e *Engine) requiredEnvironments(ctx context.Context) ([]model.Environment, string) {
	environments, err := e.Environments(ctx, e.CheckOn)
	if err != nil {
		return nil, fmt.Sprintf("check.on couldn't be resolved, so only the environments its checks planned are required: %v", err)
	}
	return environments, ""
}

// acceptanceProblem is why a port submit --accept names can't be
// accepted: it wasn't checked, it passed, no check of these files built
// it, or it's a changed port, whose failure is shared as a draft. A port
// may have several builds, its variant builds beside its default one: the
// one to accept is one that failed.
func acceptanceProblem(evidence Evidence, accept []string) error {
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
		case !evidence.Targets[i].Acceptable():
			return fmt.Errorf("--accept %s: %s is a changed port, and a changed port that fails is shared as a draft (--draft), never accepted", port, port)
		}
	}
	return nil
}

// publicationProblems applies the publication rule to evidence: every
// changed target checked and every substantive one passed, and every
// other failure accepted.
func publicationProblems(evidence Evidence, accepted []string) []string {
	var problems []string
	for _, target := range evidence.Failed() {
		name := target.Target.Target.Name
		unmet, needs := target.Unmet()
		remade := target.Remade()
		switch {
		case needs && target.Missing():
			problems = append(problems, fmt.Sprintf("%s %s, which %s hasn't; a check with %s there builds it, or share the branch as a draft (--draft)",
				name, UnmetWords(unmet), DescribeEnvironment(unmet.Environment), unmet.Needs))
		case len(remade) > 0 && target.Missing():
			problems = append(problems, fmt.Sprintf("%s's check no longer stands: since it, %s; dockhand check builds it there again, or share the branch as a draft (--draft)", name, RemadeWords(target)))
		case target.Missing():
			problems = append(problems, fmt.Sprintf("%s is changed, and no check of these files built it everywhere it's required; dockhand check builds it, or share the branch as a draft (--draft)", name))
		case !target.Acceptable():
			problems = append(problems, fmt.Sprintf("%s did not pass in %s; fix it, or share it as a draft (--draft)", name, evidence.Run.Name()))
		case !slices.Contains(accepted, name):
			problems = append(problems, fmt.Sprintf("%s (%s) did not pass in %s; acknowledge it with --accept %s if its failure is not this branch's doing", name, kindWords(target.Target), evidence.Run.Name(), name))
		}
	}
	return problems
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
