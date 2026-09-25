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
	Ports(ctx context.Context, source model.Source, directory string, platform model.Platform) ([]macports.PortInfo, error)
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
}

// PlanCheck works out the targets of a check (Design v3 §3): every subport
// of each directory the revision changes by MacPorts CI's rule, less those
// replaced, known to fail, or unsupported on a release, in dependency
// order. A directory that can't be evaluated leaves the plan unresolved;
// it is never dropped.
func (e *Engine) PlanCheck(ctx context.Context, request PlanRequest) (model.Plan, error) {
	revision := request.Revision
	plan := model.Plan{ID: model.PlanID(store.NewID("plan")), Revision: revision.ID, Environments: request.Environments, Only: request.Only, Also: request.Also, Tests: request.Tests, CreatedAt: e.now()}
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

	type candidate struct {
		target model.PlanTarget
		deps   []string
	}
	var candidates []candidate
	seen := map[string]bool{}
	excluded := map[string]int{}
	add := func(directory string, kind model.TargetKind, role model.TargetRole) {
		for _, environment := range plan.Environments {
			ports, err := reader.Ports(ctx, revision.Source, directory, environment.Platform)
			if err != nil {
				plan.Unresolved = append(plan.Unresolved, model.Unresolved{Target: model.Target{Name: directoryName(directory), Portfile: directory + "/Portfile"}, Reason: err.Error()})
				return
			}
			for i, port := range ports {
				target := model.Target{Name: port.Name, Portfile: directory + "/Portfile"}
				if i > 0 {
					target.Subport = port.Name
				}
				if reason := ineligible(port, environment.Platform); reason != "" {
					plan.Exclusions = append(plan.Exclusions, model.Exclusion{Target: target, Platform: environment.Platform, Reason: reason})
					excluded[port.Name]++
				}
				if seen[port.Name] {
					continue
				}
				seen[port.Name] = true
				var deps []string
				for _, dependency := range port.Dependencies {
					deps = append(deps, dependency.Port)
				}
				candidates = append(candidates, candidate{target: model.PlanTarget{ID: model.TargetID(port.Name), Target: target, Directory: directory, Kind: kind, Role: role}, deps: deps})
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
	for _, name := range request.Also {
		directory, err := reader.Directory(ctx, revision.Source, name)
		if err != nil {
			return plan, fmt.Errorf("--also %s: %w", name, err)
		}
		if slices.Contains(scope.Ports, directory) {
			return plan, fmt.Errorf("--also %s: the branch changes it, so it is checked already", name)
		}
		add(directory, model.Unchanged, model.Also)
	}
	if len(plan.Unresolved) > 0 {
		return plan, nil
	}

	// A target excluded on every release is not built at all.
	candidates = slices.DeleteFunc(candidates, func(c candidate) bool {
		return excluded[string(c.target.ID)] == len(plan.Environments)
	})
	names := map[string]bool{}
	for _, c := range candidates {
		names[string(c.target.ID)] = true
	}
	for i := range candidates {
		for _, dep := range candidates[i].deps {
			if names[dep] && dep != string(candidates[i].target.ID) && !slices.Contains(candidates[i].target.DependsOn, model.TargetID(dep)) {
				candidates[i].target.DependsOn = append(candidates[i].target.DependsOn, model.TargetID(dep))
			}
		}
	}

	var targets []model.PlanTarget
	for _, c := range candidates {
		targets = append(targets, c.target)
	}
	if len(request.Only) > 0 {
		if targets, err = narrow(targets, request.Only); err != nil {
			return plan, err
		}
	}
	ordered, cycle := dependencyOrder(targets)
	if cycle != nil {
		plan.Unresolved = append(plan.Unresolved, model.Unresolved{Target: model.Target{Name: cycle[0]}, Reason: "dependency cycle: " + strings.Join(cycle, " → ")})
		return plan, nil
	}
	plan.Targets = ordered
	return plan, plan.Validate()
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
// nothing under files/ changed, and the Portfile is identical once its
// revision lines are set aside. Anything else is substantive.
func (e *Engine) targetKind(ctx context.Context, before, after, directory string, changed []string) (model.TargetKind, error) {
	portfile := directory + "/Portfile"
	for _, path := range changed {
		if strings.HasPrefix(path, directory+"/") && path != portfile {
			return model.Substantive, nil
		}
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

// narrow keeps the named changed targets and adds back the changed
// prerequisites they need, marked as such.
func narrow(targets []model.PlanTarget, only []string) ([]model.PlanTarget, error) {
	byID := map[model.TargetID]model.PlanTarget{}
	for _, target := range targets {
		byID[target.ID] = target
	}
	keep := map[model.TargetID]model.TargetRole{}
	var visit func(id model.TargetID)
	visit = func(id model.TargetID) {
		for _, dep := range byID[id].DependsOn {
			if _, ok := keep[dep]; !ok && byID[dep].Role == model.Changed {
				keep[dep] = model.Prerequisite
				visit(dep)
			}
		}
	}
	for _, name := range only {
		target, ok := byID[model.TargetID(name)]
		if !ok || target.Role != model.Changed {
			return nil, fmt.Errorf("--only %s: the branch does not change it; --also builds an unchanged port", name)
		}
		keep[target.ID] = model.Changed
	}
	for _, name := range only {
		visit(model.TargetID(name))
	}
	var narrowed []model.PlanTarget
	for _, target := range targets {
		role, ok := keep[target.ID]
		if !ok && target.Role != model.Also {
			continue
		}
		if ok {
			target.Role = role
		}
		narrowed = append(narrowed, target)
	}
	return narrowed, nil
}

// dependencyOrder puts each target after the targets it depends on,
// otherwise keeping the given order as closely as it can: the first ready
// target is always placed next. It returns a cycle when there is one.
func dependencyOrder(targets []model.PlanTarget) ([]model.PlanTarget, []string) {
	byID := map[model.TargetID]model.PlanTarget{}
	for _, target := range targets {
		byID[target.ID] = target
	}
	placed := map[model.TargetID]bool{}
	ready := func(target model.PlanTarget) bool {
		for _, dep := range target.DependsOn {
			if _, present := byID[dep]; present && !placed[dep] {
				return false
			}
		}
		return true
	}
	var ordered []model.PlanTarget
	for len(ordered) < len(targets) {
		next := slices.IndexFunc(targets, func(t model.PlanTarget) bool { return !placed[t.ID] && ready(t) })
		if next < 0 {
			return nil, cycleFrom(targets, byID, placed)
		}
		target := targets[next]
		target.DependsOn = slices.DeleteFunc(slices.Clone(target.DependsOn), func(d model.TargetID) bool { _, present := byID[d]; return !present })
		ordered = append(ordered, target)
		placed[target.ID] = true
	}
	return ordered, nil
}

// cycleFrom follows unplaced dependencies from the first unplaced target
// until one repeats, and returns that loop.
func cycleFrom(targets []model.PlanTarget, byID map[model.TargetID]model.PlanTarget, placed map[model.TargetID]bool) []string {
	at := targets[slices.IndexFunc(targets, func(t model.PlanTarget) bool { return !placed[t.ID] })].ID
	var path []model.TargetID
	for !slices.Contains(path, at) {
		path = append(path, at)
		for _, dep := range byID[at].DependsOn {
			if _, present := byID[dep]; present && !placed[dep] {
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
