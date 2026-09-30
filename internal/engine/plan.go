package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/planning"
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
	where map[model.TargetID]planning.Limited
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
	evaluations := make([]planning.Evaluation, len(plan.Environments))
	for i := range evaluations {
		evaluations[i] = planning.Evaluation{}
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
				evaluated, err := planning.Evaluate(port, environment.Platform)
				if err != nil {
					plan.Unresolved = append(plan.Unresolved, model.Unresolved{Target: target, Reason: err.Error()})
					return
				}
				if !slices.ContainsFunc(candidates, func(c model.PlanTarget) bool { return c.ID == id }) {
					candidates = append(candidates, model.PlanTarget{ID: id, Target: target, Directory: directory, Kind: kind, Role: role})
				}
				evaluations[e][id] = evaluated
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

	decision, err := planning.Decide(planning.Input{Environments: plan.Environments, Candidates: candidates, Evaluations: evaluations, Only: request.Only, Where: request.where})
	if err != nil {
		return plan, err
	}
	plan.Omitted = decision.Omitted
	for _, cycle := range decision.Cycles {
		plan.Unresolved = append(plan.Unresolved, model.Unresolved{Target: model.Target{Name: cycle.Ports[0]},
			Reason: fmt.Sprintf("dependency cycle on %s: %s", describeEnvironment(cycle.Environment), strings.Join(cycle.Ports, " → "))})
	}
	if len(plan.Unresolved) > 0 {
		plan.Builds = decision.Builds
		return plan, nil
	}
	plan.Builds, plan.Targets = decision.Builds, decision.Targets
	return plan, plan.Validate()
}

func directoryName(directory string) string {
	return directory[strings.LastIndexByte(directory, '/')+1:]
}

// targetKind proves a directory's change revision-only from its source:
// nothing under files/ changed, no shared code it loads changed, and the
// Portfile is identical once its revision lines are set aside. Anything
// else is substantive (Design v3 §3).
func (e *Engine) targetKind(ctx context.Context, before, after, directory string, changed []string) (model.TargetKind, error) {
	file := directory + "/Portfile"
	for _, path := range changed {
		if strings.HasPrefix(path, directory+"/") && path != file {
			return model.Substantive, nil
		}
	}
	if e.loadsChangedSharedCode(ctx, after, file, changed) {
		return model.Substantive, nil
	}
	old, oldText, err := e.Repo.File(ctx, before, file)
	if err != nil || !old.Exists {
		return model.Substantive, nil
	}
	_, newText, err := e.Repo.File(ctx, after, file)
	if err != nil {
		return model.Substantive, nil
	}
	// Whether only revision declarations changed is the Portfile's source
	// to prove, as Tcl reads it: a "revision" in data is a change.
	if portfile.RevisionOnly(oldText, newText) {
		return model.RevisionOnly, nil
	}
	return model.Substantive, nil
}

// loadsChangedSharedCode reports whether changed shared code under
// _resources can reach a Portfile, as its source says, answering yes to
// anything the source can't settle. A change outside port1.0/group reaches
// every port, since Base itself reads those files: the compiler lists and
// the mirror sites. A changed PortGroup reaches the ports that load it,
// directly or through another PortGroup. A PortGroup line that doesn't
// spell its name and version literally, or a file that names _resources
// itself, could load anything.
func (e *Engine) loadsChangedSharedCode(ctx context.Context, tree, start string, changed []string) bool {
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
	queue, seen := []string{start}, map[string]bool{start: true}
	for len(queue) > 0 {
		file, text, err := e.Repo.File(ctx, tree, queue[0])
		queue = queue[1:]
		if err != nil || !file.Exists {
			return true
		}
		references, conclusive := portfile.PortGroupReferences(text)
		if !conclusive {
			return true
		}
		for _, reference := range references {
			next := reference.Path()
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
