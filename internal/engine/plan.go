package engine

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// PortReader reads ports from a source tree: the ports a directory defines
// on a platform, main port first, and the directory that defines a port by
// name. MacPorts' own evaluator is the real one.
type PortReader interface {
	// Ports reads a directory as it reads in an environment: on its
	// platform, with its developer tools where it states them.
	Ports(ctx context.Context, source model.Source, directory string, environment model.Environment) ([]macports.PortInfo, error)
	Directory(ctx context.Context, source model.Source, name string) (string, error)
}

// PlanRequest asks what a check of a revision would build.
type PlanRequest struct {
	Revision     model.Revision
	Environments []model.Environment
	// Only narrows the check to these changed ports, and their changed
	// prerequisites.
	Only []string
	// Also adds unchanged ports, built against the branch.
	Also  []string
	Tests model.TestPolicy
	// Fresh builds every target, reusing no earlier build's result.
	Fresh bool
	// directories are Also ports' directories where the caller knows them
	// already, as a baseline does from the check it explains, so they
	// aren't looked up by name again.
	directories map[string]string
	// alone builds only the Also ports named, not the rest of their
	// directories' subports, as a baseline does.
	alone bool
	// where builds Also ports only in some environments, as a baseline
	// rebuilds a port only where it failed; elsewhere they are left out.
	where map[string]rebuild
}

// rebuild is where a port is built, and why not elsewhere.
type rebuild struct {
	environments []model.Environment
	elsewhere    string
}

// PlanCheck works out the targets of a check (Design v3 §3): every subport
// of each directory the revision changes by MacPorts CI's rule, less those
// replaced, known to fail, or unsupported on a release, in dependency
// order. A directory that can't be evaluated leaves the plan unresolved;
// it is never dropped.
func (e *Engine) PlanCheck(ctx context.Context, request PlanRequest) (model.Plan, error) {
	revision := request.Revision
	// A policy that isn't one of the three is refused before anything is
	// evaluated.
	if request.Tests != "" && !request.Tests.Valid() {
		return model.Plan{}, fmt.Errorf("--tests %q is not declared, required, or skip", request.Tests)
	}
	plan := model.Plan{ID: model.PlanID(store.NewID("plan")), Revision: revision.ID, Environments: request.Environments, Only: request.Only, Also: request.Also, Tests: request.Tests, Fresh: request.Fresh, CreatedAt: e.now()}
	if plan.Tests == "" {
		plan.Tests = model.TestsDeclared
	}
	if len(plan.Environments) == 0 {
		return plan, fmt.Errorf("a check needs somewhere to build: name a provider with --on")
	}
	reader, err := e.portReader()
	if err != nil {
		return plan, err
	}
	trees, err := e.Repo.CommitTrees(ctx, []string{string(revision.Source.Base)})
	if err != nil {
		return plan, err
	}
	baseTree := trees[string(revision.Source.Base)]
	changed, err := e.Repo.ChangedPaths(ctx, baseTree, string(revision.Source.Tree))
	if err != nil {
		return plan, err
	}
	scope := ScopeOf(changed)

	// Each environment evaluates the ports for itself: a port may be
	// defined, eligible, need Xcode, or need a changed library on one
	// platform and not another.
	type evaluation struct {
		defined    map[model.TargetID]bool
		ineligible map[model.TargetID]string
		deps       map[model.TargetID][]model.TargetID
		xcode      map[model.TargetID]bool
		untested   map[model.TargetID]bool
	}
	evaluations := make([]evaluation, len(plan.Environments))
	for i := range evaluations {
		evaluations[i] = evaluation{defined: map[model.TargetID]bool{}, ineligible: map[model.TargetID]string{}, deps: map[model.TargetID][]model.TargetID{}, xcode: map[model.TargetID]bool{}, untested: map[model.TargetID]bool{}}
	}
	// candidates are every port some environment defined, as the branch
	// sees it, in the order the scope and --also name them.
	var candidates []model.PlanTarget
	add := func(directory string, kind model.TargetKind, role model.TargetRole) {
		for e, environment := range plan.Environments {
			ports, err := reader.Ports(ctx, revision.Source, directory, environment)
			if err != nil {
				plan.Unresolved = append(plan.Unresolved, model.Unresolved{Target: model.Target{Name: directoryName(directory), Portfile: directory + "/Portfile"}, Reason: err.Error()})
				return
			}
			for i, port := range ports {
				if role == model.Also && request.alone && !slices.Contains(request.Also, port.Name) {
					continue
				}
				id := model.TargetID(port.Name)
				target := model.Target{Name: port.Name, Portfile: directory + "/Portfile"}
				if i > 0 {
					target.Subport = port.Name
				}
				// Whether it needs Xcode is the environment's own answer,
				// with its tools (decision 7).
				needsXcode, err := port.Bool("use_xcode")
				if err != nil {
					plan.Unresolved = append(plan.Unresolved, model.Unresolved{Target: target, Reason: err.Error()})
					return
				}
				if !slices.ContainsFunc(candidates, func(c model.PlanTarget) bool { return c.ID == id }) {
					candidates = append(candidates, model.PlanTarget{ID: id, Target: target, Directory: directory, Kind: kind, Role: role})
				}
				evaluated := evaluations[e]
				evaluated.defined[id] = true
				if reason := ineligible(port, environment.Platform); reason != "" {
					evaluated.ineligible[id] = reason
				}
				for _, dependency := range port.Dependencies {
					evaluated.deps[id] = append(evaluated.deps[id], model.TargetID(dependency.Port))
				}
				evaluated.xcode[id] = needsXcode
				// Whether it declares tests is MacPorts' reading of test.run;
				// one that can't be read, or wasn't, is left unsaid.
				if _, set := port.Options["dockhand.test_run"]; set {
					if tests, err := port.Bool("dockhand.test_run"); err == nil && !tests {
						evaluated.untested[id] = true
					}
				}
			}
		}
	}
	for _, directory := range scope.Ports {
		kind, err := e.targetKind(ctx, baseTree, string(revision.Source.Tree), directory, changed)
		if err != nil {
			return plan, err
		}
		add(directory, kind, model.Changed)
	}
	// A directory is evaluated once, however many of its ports are named:
	// twice would count its exclusions twice.
	var also []string
	for _, name := range request.Also {
		directory, known := request.directories[name]
		if !known {
			if directory, err = reader.Directory(ctx, revision.Source, name); err != nil {
				return plan, fmt.Errorf("--also %s: %w", name, err)
			}
		}
		if slices.Contains(scope.Ports, directory) {
			return plan, fmt.Errorf("--also %s: the branch changes it, so it is checked already", name)
		}
		if slices.Contains(also, directory) {
			continue
		}
		also = append(also, directory)
		add(directory, model.Unchanged, model.Also)
	}
	if len(plan.Unresolved) > 0 {
		return plan, nil
	}

	// What each environment rules out, and why: a port its evaluation
	// didn't define, whichever other environment defined it, and a port
	// MacPorts CI would exclude there.
	reasons := make([]map[model.TargetID]string, len(plan.Environments))
	for e := range plan.Environments {
		reasons[e] = map[model.TargetID]string{}
		for _, c := range candidates {
			asked, limited := request.where[string(c.ID)]
			switch {
			case !evaluations[e].defined[c.ID]:
				reasons[e][c.ID] = "not defined there"
			case evaluations[e].ineligible[c.ID] != "":
				reasons[e][c.ID] = evaluations[e].ineligible[c.ID]
			case limited && !slices.Contains(asked.environments, plan.Environments[e]):
				reasons[e][c.ID] = asked.elsewhere
			}
		}
	}
	// A target ruled out everywhere is not built at all.
	built := slices.DeleteFunc(slices.Clone(candidates), func(c model.PlanTarget) bool {
		return !slices.ContainsFunc(reasons, func(ruled map[model.TargetID]string) bool { return ruled[c.ID] == "" })
	})
	builtAnywhere := func(id model.TargetID) bool {
		return slices.ContainsFunc(built, func(c model.PlanTarget) bool { return c.ID == id })
	}
	// What each target needs built first in each environment: the targets
	// built there that it depends on there. Their union, over every
	// environment, is what --only adds back.
	needs := make([]map[model.TargetID][]model.TargetID, len(plan.Environments))
	union := map[model.TargetID][]model.TargetID{}
	for e := range plan.Environments {
		needs[e] = map[model.TargetID][]model.TargetID{}
		for _, c := range built {
			if reasons[e][c.ID] != "" {
				continue
			}
			for _, dep := range evaluations[e].deps[c.ID] {
				if dep == c.ID || !builtAnywhere(dep) || reasons[e][dep] != "" || slices.Contains(needs[e][c.ID], dep) {
					continue
				}
				needs[e][c.ID] = append(needs[e][c.ID], dep)
				if !slices.Contains(union[c.ID], dep) {
					union[c.ID] = append(union[c.ID], dep)
				}
			}
		}
	}
	targets := built
	if len(request.Only) > 0 {
		if targets, plan.Omitted, err = narrow(built, union, request.Only); err != nil {
			return plan, err
		}
	}

	// Each environment's plan: what it builds, in its own dependency
	// order, so dependencies that run opposite ways on two releases are
	// no cycle; what those need there; and what it rules out, the targets
	// --only left out included, since submit requires them wherever they
	// aren't.
	for e, environment := range plan.Environments {
		planned := model.EnvironmentPlan{Environment: environment}
		var members []model.TargetID
		for _, target := range targets {
			if reasons[e][target.ID] == "" {
				members = append(members, target.ID)
			}
		}
		dependencies := map[model.TargetID][]model.TargetID{}
		for _, id := range members {
			for _, dep := range needs[e][id] {
				if slices.Contains(members, dep) {
					dependencies[id] = append(dependencies[id], dep)
				}
			}
		}
		order, cycle := dependencyOrder(members, dependencies)
		if cycle != nil {
			plan.Unresolved = append(plan.Unresolved, model.Unresolved{Target: model.Target{Name: cycle[0]},
				Reason: fmt.Sprintf("dependency cycle on %s: %s", describeEnvironment(environment), strings.Join(cycle, " → "))})
			continue
		}
		planned.Order = order
		if len(dependencies) > 0 {
			planned.Dependencies = dependencies
		}
		for _, id := range order {
			if evaluations[e].xcode[id] {
				planned.NeedsXcode = append(planned.NeedsXcode, id)
			}
			if evaluations[e].untested[id] {
				planned.Untested = append(planned.Untested, id)
			}
		}
		for _, c := range candidates {
			if reason := reasons[e][c.ID]; reason != "" {
				planned.Exclusions = append(planned.Exclusions, model.Exclusion{Target: c.Target, Reason: reason})
			}
		}
		// What it needs there decides what it can't build.
		planned.Unmet = unmetNeeds(planned)
		plan.Builds = append(plan.Builds, planned)
	}
	if len(plan.Unresolved) > 0 {
		return plan, nil
	}
	// The plan's own list is the environments' orders, merged: the first
	// environment's, and what each other one adds, after what comes before
	// it there.
	var order []model.TargetID
	for _, planned := range plan.Builds {
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
	for _, id := range order {
		plan.Targets = append(plan.Targets, targets[slices.IndexFunc(targets, func(t model.PlanTarget) bool { return t.ID == id })])
	}
	return plan, plan.Validate()
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

func directoryName(directory string) string {
	return directory[strings.LastIndexByte(directory, '/')+1:]
}

// ineligible says why a port is not built on a platform, following
// MacPorts CI: replaced ports, ports marked known_fail, and ports whose
// supported_archs exclude the platform's.
func ineligible(port macports.PortInfo, platform model.Platform) string {
	if by := strings.TrimSpace(port.Options["replaced_by"]); by != "" {
		return "replaced by " + by
	}
	switch strings.ToLower(strings.TrimSpace(port.Options["known_fail"])) {
	case "yes", "1", "true":
		return "known_fail"
	}
	archs := strings.Fields(port.Options["supported_archs"])
	if len(archs) > 0 && platform.Architecture != "" && !slices.Contains(archs, "noarch") && !slices.Contains(archs, platform.Architecture) {
		return "supported_archs " + strings.Join(archs, " ") + " only"
	}
	return ""
}

var revisionDeclaration = regexp.MustCompile(`^\s*revision\s+\S+\s*$`)

// targetKind proves a directory's change revision-only from its source:
// nothing under files/ changed, no shared code it loads changed, and the
// Portfile is identical once its revision lines are set aside. Anything
// else is substantive (Design v3 §3).
func (e *Engine) targetKind(ctx context.Context, before, after, directory string, changed []string) (model.TargetKind, error) {
	portfile := directory + "/Portfile"
	for _, path := range changed {
		if strings.HasPrefix(path, directory+"/") && path != portfile {
			return model.Substantive, nil
		}
	}
	if e.loadsChangedSharedCode(ctx, after, portfile, changed) {
		return model.Substantive, nil
	}
	old, oldText, err := e.Repo.File(ctx, before, portfile)
	if err != nil || !old.Exists {
		return model.Substantive, nil
	}
	_, newText, err := e.Repo.File(ctx, after, portfile)
	if err != nil {
		return model.Substantive, nil
	}
	strip := func(text []byte) []string {
		return slices.DeleteFunc(strings.Split(string(text), "\n"), revisionDeclaration.MatchString)
	}
	if slices.Equal(strip(oldText), strip(newText)) {
		return model.RevisionOnly, nil
	}
	return model.Substantive, nil
}

var (
	portGroup = regexp.MustCompile(`\bPortGroup\b(.*)`)
	literal   = regexp.MustCompile(`^[A-Za-z0-9_.+-]+$`)
)

// loadsChangedSharedCode reports whether changed shared code under
// _resources can reach a Portfile, as its source says, answering yes to
// anything the source can't settle. A change outside port1.0/group reaches
// every port, since Base itself reads those files: the compiler lists and
// the mirror sites. A changed PortGroup reaches the ports that load it,
// directly or through another PortGroup. A PortGroup line that doesn't
// spell its name and version literally, or a file that names _resources
// itself, could load anything.
func (e *Engine) loadsChangedSharedCode(ctx context.Context, tree, portfile string, changed []string) bool {
	groups := map[string]bool{}
	for _, path := range changed {
		if _, ok := macports.PortGroupAt(path); ok {
			groups[path] = true
		} else if strings.HasPrefix(path, macports.ResourcesDirectory+"/") {
			return true
		}
	}
	if len(groups) == 0 {
		return false
	}
	queue, seen := []string{portfile}, map[string]bool{portfile: true}
	for len(queue) > 0 {
		file, text, err := e.Repo.File(ctx, tree, queue[0])
		queue = queue[1:]
		if err != nil || !file.Exists {
			return true
		}
		for _, line := range strings.Split(string(text), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if strings.Contains(line, macports.ResourcesDirectory) {
				return true
			}
			m := portGroup.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			fields := strings.Fields(strings.TrimRight(m[1], "}; "))
			if len(fields) < 2 || !literal.MatchString(fields[0]) || !literal.MatchString(fields[1]) {
				return true
			}
			next := macports.PortGroup{Name: fields[0], Version: fields[1]}.Path()
			if groups[next] {
				return true
			}
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return false
}

// narrow keeps the named changed targets and adds back the changed
// prerequisites they need in any environment, marked as such. It returns
// the changed targets it left out, which submission still requires.
func narrow(targets []model.PlanTarget, needs map[model.TargetID][]model.TargetID, only []string) (narrowed, omitted []model.PlanTarget, err error) {
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
	for _, name := range only {
		target, ok := byID[model.TargetID(name)]
		if !ok || target.Role != model.Changed {
			return nil, nil, fmt.Errorf("--only %s: the branch does not change it; --also builds an unchanged port", name)
		}
		keep[target.ID] = model.Changed
	}
	for _, name := range only {
		visit(model.TargetID(name))
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
