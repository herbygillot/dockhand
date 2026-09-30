// Package planning decides what a check builds, where, and in what order,
// from what each environment's evaluation says of the ports it could build
// (Design v3 §3). It reads nothing itself: the engine evaluates the ports
// and decides which directories changed and how, and planning decides the
// rest, in named phases, each a function of the ones before it.
package planning

import (
	"fmt"
	"slices"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// Evaluated is what one environment's evaluation says of one port: whether
// MacPorts CI would build it there, what it depends on there, whether it
// needs Xcode with the environment's tools, and whether it declares tests.
type Evaluated struct {
	Eligibility  macports.Eligibility
	Dependencies []Dependency
	NeedsXcode   bool
	// Untested is a port whose test.run MacPorts reads as off; one it
	// couldn't read, or didn't, is left unsaid.
	Untested bool
	// Git is what a port fetched with Git declares it fetches there, which
	// the engine resolves to the commit its build is expected to fetch;
	// nil for a port fetched otherwise.
	Git *model.GitSource
}

// Dependency is a port another depends on. ByFile is one Base would find
// met by a file where the file is there and no port owns it (a lib:,
// bin:, or path: dependency, and none by port:), which a dependency by
// port on the same port overrides.
type Dependency struct {
	Port   model.TargetID
	ByFile bool
}

// Evaluate reads what planning needs of a port as an environment on the
// platform evaluated it. What can't be read is an error, which leaves the
// port unresolved rather than excluded or built.
func Evaluate(port macports.PortInfo, platform model.Platform) (Evaluated, error) {
	// Whether it needs Xcode is the environment's own answer, with its
	// tools (decision 7).
	needsXcode, err := port.Bool("use_xcode")
	if err != nil {
		return Evaluated{}, err
	}
	// Whether MacPorts CI would build it here is macports' to say.
	eligibility, err := macports.BuildEligibility(port, platform)
	if err != nil {
		return Evaluated{}, err
	}
	evaluated := Evaluated{Eligibility: eligibility, NeedsXcode: needsXcode}
	for _, dependency := range port.Dependencies {
		id := model.TargetID(dependency.Port)
		if i := slices.IndexFunc(evaluated.Dependencies, func(d Dependency) bool { return d.Port == id }); i >= 0 {
			evaluated.Dependencies[i].ByFile = evaluated.Dependencies[i].ByFile && dependency.MetByFile()
			continue
		}
		evaluated.Dependencies = append(evaluated.Dependencies, Dependency{Port: id, ByFile: dependency.MetByFile()})
	}
	if declares, known := port.DeclaresTests(); known && !declares {
		evaluated.Untested = true
	}
	source, git, err := port.GitSource()
	if err != nil {
		return Evaluated{}, err
	}
	if git {
		evaluated.Git = &source
	}
	return evaluated, nil
}

// Evaluation is one environment's reading of the candidates: each port it
// defined. A candidate it lacks is one it doesn't define.
type Evaluation map[model.TargetID]Evaluated

// Limited builds an extra only in some environments, as a baseline
// rebuilds a port only where it failed, and says why not elsewhere.
type Limited struct {
	Environments []model.Environment
	Elsewhere    string
}

// Input is what a plan is decided from.
type Input struct {
	Environments []model.Environment
	// Candidates are every port some environment defined, in the order the
	// changed directories and the extras name them.
	Candidates []model.PlanTarget
	// Evaluations are one per environment, in Environments' order.
	Evaluations []Evaluation
	// Only narrows the plan to these changed ports, and the changed
	// prerequisites they need.
	Only []string
	// Where limits extras to some environments.
	Where map[model.TargetID]Limited
}

// Cycle is a dependency loop among the targets one environment builds,
// which leaves the plan unresolved.
type Cycle struct {
	Environment model.Environment
	Ports       []string
}

// Decision is what a plan builds: its targets, in an order every
// environment's agrees with, the changed ones --only left out, and each
// environment's own plan. Where an environment's targets depend on each
// other in a loop, it has its cycle and no plan.
type Decision struct {
	Targets []model.PlanTarget
	Omitted []model.PlanTarget
	Builds  []model.EnvironmentPlan
	Cycles  []Cycle
}

// Decide plans the candidates, phase by phase: what each environment rules
// out, what is built anywhere, what each target needs first where it's
// built, what --only keeps, each environment's plan, and the plan's own
// order, merged from theirs.
func Decide(input Input) (Decision, error) {
	if len(input.Evaluations) != len(input.Environments) {
		return Decision{}, fmt.Errorf("%d evaluations for %d environments", len(input.Evaluations), len(input.Environments))
	}
	reasons := Exclusions(input)
	built := Built(input.Candidates, reasons)
	needs, union := Needs(built, reasons, input.Evaluations)
	targets := built
	var decision Decision
	if len(input.Only) > 0 {
		var err error
		if targets, decision.Omitted, err = Narrow(built, union, input.Only); err != nil {
			return Decision{}, err
		}
	}
	for e, environment := range input.Environments {
		planned, cycle := EnvironmentPlan(environment, targets, input.Candidates, reasons[e], needs[e], input.Evaluations[e])
		if cycle != nil {
			decision.Cycles = append(decision.Cycles, Cycle{Environment: environment, Ports: cycle})
			continue
		}
		decision.Builds = append(decision.Builds, planned)
	}
	if len(decision.Cycles) > 0 {
		return decision, nil
	}
	decision.Targets = Merge(decision.Builds, targets)
	return decision, nil
}

// Exclusions are what each environment rules out, and why: a port its
// evaluation didn't define, whichever other environment defined it, a port
// MacPorts CI would exclude there, and an extra limited to elsewhere.
func Exclusions(input Input) []map[model.TargetID]string {
	reasons := make([]map[model.TargetID]string, len(input.Environments))
	for e, environment := range input.Environments {
		reasons[e] = map[model.TargetID]string{}
		for _, c := range input.Candidates {
			evaluated, defined := input.Evaluations[e][c.ID]
			limit, limited := input.Where[c.ID]
			switch {
			case !defined:
				reasons[e][c.ID] = "not defined there"
			case !evaluated.Eligibility.Eligible():
				reasons[e][c.ID] = evaluated.Eligibility.Reason()
			case limited && !slices.Contains(limit.Environments, environment):
				reasons[e][c.ID] = limit.Elsewhere
			}
		}
	}
	return reasons
}

// Built are the candidates some environment doesn't rule out: one ruled
// out everywhere is not built at all.
func Built(candidates []model.PlanTarget, reasons []map[model.TargetID]string) []model.PlanTarget {
	return slices.DeleteFunc(slices.Clone(candidates), func(c model.PlanTarget) bool {
		return !slices.ContainsFunc(reasons, func(ruled map[model.TargetID]string) bool { return ruled[c.ID] == "" })
	})
}

// Needs are what each target needs built first in each environment: the
// targets built there that it depends on there. Their union, over every
// environment, is what --only adds back.
func Needs(built []model.PlanTarget, reasons []map[model.TargetID]string, evaluations []Evaluation) (needs []map[model.TargetID][]Dependency, union map[model.TargetID][]model.TargetID) {
	builtAnywhere := func(id model.TargetID) bool {
		return slices.ContainsFunc(built, func(c model.PlanTarget) bool { return c.ID == id })
	}
	// A dependency names a port: it's the port's default build where the
	// plan has one, and otherwise its one build, as --variants builds a
	// port with its variants in place of its defaults.
	resolve := func(dep model.TargetID) model.TargetID {
		if builtAnywhere(dep) {
			return dep
		}
		var builds []model.TargetID
		for _, c := range built {
			if model.TargetID(c.Target.Name) == dep {
				builds = append(builds, c.ID)
			}
		}
		if len(builds) == 1 {
			return builds[0]
		}
		return dep
	}
	needs = make([]map[model.TargetID][]Dependency, len(evaluations))
	union = map[model.TargetID][]model.TargetID{}
	for e := range evaluations {
		needs[e] = map[model.TargetID][]Dependency{}
		for _, c := range built {
			if reasons[e][c.ID] != "" {
				continue
			}
			for _, dep := range evaluations[e][c.ID].Dependencies {
				dep.Port = resolve(dep.Port)
				if dep.Port == c.ID || !builtAnywhere(dep.Port) || reasons[e][dep.Port] != "" {
					continue
				}
				needs[e][c.ID] = append(needs[e][c.ID], dep)
				if !slices.Contains(union[c.ID], dep.Port) {
					union[c.ID] = append(union[c.ID], dep.Port)
				}
			}
		}
	}
	return needs, union
}

// EnvironmentPlan is one environment's plan: what it builds, in its own
// dependency order, so dependencies that run opposite ways on two releases
// are no cycle; what those need there; and what it rules out, the targets
// --only left out included, since submit requires them wherever they
// aren't. A loop among what it builds is returned instead of a plan,
// unless a dependency met by a file closes it: Base drops that dependency
// where the file is there, and would loop itself where it isn't, so the
// plan drops it and keeps the order the rest make.
func EnvironmentPlan(environment model.Environment, targets, candidates []model.PlanTarget, reasons map[model.TargetID]string, needs map[model.TargetID][]Dependency, evaluation Evaluation) (model.EnvironmentPlan, []string) {
	planned := model.EnvironmentPlan{Environment: environment}
	var members []model.TargetID
	for _, target := range targets {
		if reasons[target.ID] == "" {
			members = append(members, target.ID)
		}
	}
	dependencies := map[model.TargetID][]model.TargetID{}
	byFile := map[[2]model.TargetID]bool{}
	for _, id := range members {
		for _, dep := range needs[id] {
			if slices.Contains(members, dep.Port) {
				dependencies[id] = append(dependencies[id], dep.Port)
				byFile[[2]model.TargetID{id, dep.Port}] = dep.ByFile
			}
		}
	}
	order, cycle := dependencyOrder(members, dependencies)
	for cycle != nil {
		// The loop's first dependency met by a file goes, and the order is
		// found again; a loop of dependencies by port alone is refused.
		i := 0
		for i < len(cycle)-1 && !byFile[[2]model.TargetID{model.TargetID(cycle[i]), model.TargetID(cycle[i+1])}] {
			i++
		}
		if i == len(cycle)-1 {
			return model.EnvironmentPlan{}, cycle
		}
		from, to := model.TargetID(cycle[i]), model.TargetID(cycle[i+1])
		dependencies[from] = slices.DeleteFunc(dependencies[from], func(id model.TargetID) bool { return id == to })
		if len(dependencies[from]) == 0 {
			delete(dependencies, from)
		}
		order, cycle = dependencyOrder(members, dependencies)
	}
	planned.Order = order
	if len(dependencies) > 0 {
		planned.Dependencies = dependencies
	}
	for _, id := range order {
		if evaluation[id].NeedsXcode {
			planned.NeedsXcode = append(planned.NeedsXcode, id)
		}
		if evaluation[id].Untested {
			planned.Untested = append(planned.Untested, id)
		}
		if source := evaluation[id].Git; source != nil {
			if planned.Git == nil {
				planned.Git = map[model.TargetID]model.GitSource{}
			}
			planned.Git[id] = *source
		}
	}
	for _, c := range candidates {
		if reason := reasons[c.ID]; reason != "" {
			planned.Exclusions = append(planned.Exclusions, model.Exclusion{Target: c.Target, Reason: reason})
		}
	}
	// What it needs there decides what it can't build.
	planned.Unmet = unmetNeeds(planned)
	return planned, nil
}

// Merge is the plan's own list of targets: the environments' orders,
// merged, the first environment's, and what each other one adds, after
// what comes before it there.
func Merge(builds []model.EnvironmentPlan, targets []model.PlanTarget) []model.PlanTarget {
	var order []model.TargetID
	for _, planned := range builds {
		at := 0
		for _, id := range planned.Order {
			if i := slices.Index(order, id); i >= 0 {
				at = i + 1
				continue
			}
			order = slices.Insert(order, at, id)
			at++
		}
	}
	var merged []model.PlanTarget
	for _, id := range order {
		merged = append(merged, targets[slices.IndexFunc(targets, func(t model.PlanTarget) bool { return t.ID == id })])
	}
	return merged
}

// unmetNeeds are the targets an environment can't build. With the Command
// Line Tools alone, that is a target that needs Xcode, and one whose
// prerequisite does: the plan builds a changed prerequisite from source
// before its dependents, never from an archive, so what it needs, they
// need. An environment whose tools are Xcode, or unstated, builds them
// all.
func unmetNeeds(planned model.EnvironmentPlan) []model.Unmet {
	if planned.Environment.DeveloperTools != model.DeveloperToolsCommandLine {
		return nil
	}
	var unmet []model.Unmet
	// cause is, for each target that needs Xcode, the one that needs it
	// itself. The order puts a prerequisite before its dependents, so it
	// is settled first.
	cause := map[model.TargetID]model.TargetID{}
	for _, id := range planned.Order {
		if slices.Contains(planned.NeedsXcode, id) {
			cause[id] = id
			unmet = append(unmet, model.Unmet{Target: id, Environment: planned.Environment, Needs: model.RequiresXcode})
			continue
		}
		for _, prerequisite := range planned.Dependencies[id] {
			if through, ok := cause[prerequisite]; ok {
				cause[id] = through
				unmet = append(unmet, model.Unmet{Target: id, Environment: planned.Environment, Needs: model.RequiresXcode, Through: through})
				break
			}
		}
	}
	return unmet
}

// Narrow keeps the named changed targets and adds back the changed
// prerequisites they need in any environment, marked as such. It returns
// the changed targets it left out, which submission still requires.
func Narrow(targets []model.PlanTarget, needs map[model.TargetID][]model.TargetID, only []string) (narrowed, omitted []model.PlanTarget, err error) {
	byID := map[model.TargetID]model.PlanTarget{}
	for _, target := range targets {
		byID[target.ID] = target
	}
	keep := map[model.TargetID]model.TargetRole{}
	var visit func(id model.TargetID)
	visit = func(id model.TargetID) {
		for _, dep := range needs[id] {
			if _, ok := keep[dep]; !ok && byID[dep].Role == model.Changed {
				keep[dep] = model.Prerequisite
				visit(dep)
			}
		}
	}
	// A port named is every build of it the plan has, its variant builds
	// with its default one.
	named := func(name string) []model.PlanTarget {
		return slices.DeleteFunc(slices.Clone(targets), func(t model.PlanTarget) bool { return t.Target.Name != name || t.Role != model.Changed })
	}
	for _, name := range only {
		builds := named(name)
		if len(builds) == 0 {
			return nil, nil, fmt.Errorf("--only %s: the branch does not change it; --also builds an unchanged port", name)
		}
		for _, target := range builds {
			keep[target.ID] = model.Changed
		}
	}
	for _, name := range only {
		for _, target := range named(name) {
			visit(target.ID)
		}
	}
	for _, target := range targets {
		role, ok := keep[target.ID]
		if !ok && target.Role != model.Also {
			omitted = append(omitted, target)
			continue
		}
		if ok {
			target.Role = role
		}
		narrowed = append(narrowed, target)
	}
	return narrowed, omitted, nil
}

// dependencyOrder puts each target after the targets it needs, otherwise
// keeping the given order as closely as it can: the first ready target is
// always placed next. It returns a cycle when there is one.
func dependencyOrder(ids []model.TargetID, needs map[model.TargetID][]model.TargetID) ([]model.TargetID, []string) {
	placed := map[model.TargetID]bool{}
	ready := func(id model.TargetID) bool {
		for _, dep := range needs[id] {
			if !placed[dep] {
				return false
			}
		}
		return true
	}
	var ordered []model.TargetID
	for len(ordered) < len(ids) {
		next := slices.IndexFunc(ids, func(id model.TargetID) bool { return !placed[id] && ready(id) })
		if next < 0 {
			return nil, cycleFrom(ids, needs, placed)
		}
		ordered = append(ordered, ids[next])
		placed[ids[next]] = true
	}
	return ordered, nil
}

// cycleFrom follows unplaced dependencies from the first unplaced target
// until one repeats, and returns that loop.
func cycleFrom(ids []model.TargetID, needs map[model.TargetID][]model.TargetID, placed map[model.TargetID]bool) []string {
	at := ids[slices.IndexFunc(ids, func(id model.TargetID) bool { return !placed[id] })]
	var path []model.TargetID
	for !slices.Contains(path, at) {
		path = append(path, at)
		for _, dep := range needs[at] {
			if !placed[dep] {
				at = dep
				break
			}
		}
	}
	var cycle []string
	for _, id := range path[slices.Index(path, at):] {
		cycle = append(cycle, string(id))
	}
	return append(cycle, string(at))
}
