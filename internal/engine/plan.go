package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/planning"
	"github.com/herbygillot/dockhand/internal/progress"
	"github.com/herbygillot/dockhand/internal/store"
)

// PortReader reads ports from a source tree: the ports a directory defines
// on a platform, main port first, and the directory that defines a port by
// name. MacPorts' own evaluator is the real one.
type PortReader interface {
	// Ports reads a directory as it reads in an environment: on its
	// platform, with its developer tools where it states them, and with the
	// variants chosen, its defaults where none are.
	Ports(ctx context.Context, source model.Source, directory string, environment model.Environment, variants map[string]bool) ([]macports.PortInfo, error)
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
	// Variants builds the one port the check selects with these variants
	// instead of its defaults; EachVariant builds it with its defaults,
	// and then with each variant it declares (VariantBuilds).
	Variants    map[string]bool
	EachVariant bool
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
	plan := model.Plan{ID: model.PlanID(store.NewID("plan")), Revision: revision.ID, Environments: request.Environments, Only: request.Only, Also: request.Also, Tests: request.Tests, Fresh: request.Fresh,
		Variants: model.Target{Variants: request.Variants}.VariantSpec(), EachVariant: request.EachVariant, CreatedAt: e.now()}
	if len(request.Variants) > 0 && request.EachVariant {
		return plan, errors.New("--variants each builds every variant; it takes no others")
	}
	if len(request.Variants) > 0 || request.EachVariant {
		// MacPorts' workflow builds each port's default variants alone.
		if slices.ContainsFunc(request.Environments, func(environment model.Environment) bool { return environment.Provider == buildenv.GitHub }) {
			return plan, errors.New("--variants builds on tart or your command: GitHub's workflow builds default variants only")
		}
	}
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
	scope := macports.ScopeOf(changed)

	// Each environment evaluates the ports for itself: a port may be
	// defined, eligible, need Xcode, or need a changed library on one
	// platform and not another.
	evaluations := make([]planning.Evaluation, len(plan.Environments))
	// infos are the ports as each environment read them, for the variants
	// they declare.
	infos := make([]map[model.TargetID]macports.PortInfo, len(plan.Environments))
	for i := range evaluations {
		evaluations[i], infos[i] = planning.Evaluation{}, map[model.TargetID]macports.PortInfo{}
	}
	// candidates are every port some environment defined, as the branch
	// sees it, in the order the scope and --also name them.
	var candidates []model.PlanTarget
	add := func(directory string, kind model.TargetKind, role model.TargetRole) {
		for e, environment := range plan.Environments {
			ports, err := reader.Ports(ctx, revision.Source, directory, environment, nil)
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
				evaluations[e][id], infos[e][id] = evaluated, port
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
	if len(request.Variants) > 0 || request.EachVariant {
		if candidates, err = planVariants(ctx, reader, revision.Source, request, &plan, candidates, evaluations, infos); err != nil || len(plan.Unresolved) > 0 {
			return plan, err
		}
	}

	throughUnchanged(ctx, reader, revision.Source, candidates, evaluations)
	decision, err := planning.Decide(planning.Input{Environments: plan.Environments, Candidates: candidates, Evaluations: evaluations, Only: request.Only, Where: request.where})
	if err != nil {
		return plan, err
	}
	plan.Omitted = decision.Omitted
	for _, cycle := range decision.Cycles {
		plan.Unresolved = append(plan.Unresolved, model.Unresolved{Target: model.Target{Name: cycle.Ports[0]},
			Reason: fmt.Sprintf("dependency cycle on %s: %s", describeEnvironment(cycle.Environment), strings.Join(cycle.Ports, " → "))})
	}
	if err := e.resolveGitSources(ctx, decision.Builds); err != nil {
		return plan, err
	}
	if len(plan.Unresolved) > 0 {
		plan.Builds = decision.Builds
		return plan, nil
	}
	plan.Builds, plan.Targets = decision.Builds, decision.Targets
	return plan, plan.Validate()
}

// gitResolveTimeout bounds reading one repository's refs as a check is
// planned, so a host that doesn't answer can't hold the check.
const gitResolveTimeout = time.Minute

// resolveGitSources resolves what each Git-fetched target's build is
// expected to fetch (batch 20): the commit its git.branch names now, in a
// fresh clone of its git.url (git.CloneCheckout). A Portfile keeps its
// tag, and the plan the commit, which the build checks it fetched and its
// result records. Each repository and ref is read once for the plan, so
// every environment expects the same commit. One that can't be read is
// said to be, and its build expected to fetch nothing in particular: its
// result says what it fetched, and stands for no later check's. A target
// --only left out is resolved too (planning.OmittedSources): nothing
// builds it, but an earlier check's result of it stands only for the
// commit its tag names now, and that is read now or never, since evidence
// is judged from the store alone.
func (e *Engine) resolveGitSources(ctx context.Context, builds []model.EnvironmentPlan) error {
	resolved := map[[2]string]model.GitSource{}
	for _, planned := range builds {
		for _, id := range slices.Sorted(maps.Keys(planned.Git)) {
			source := planned.Git[id]
			key := [2]string{source.URL, source.Ref}
			if _, done := resolved[key]; !done {
				progress.VerboseReport(ctx, "%s is fetched with Git: reading which commit %s names in %s", id, refWords(source.Ref), source.URL)
				resolved[key] = e.resolveGit(ctx, source)
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			planned.Git[id] = resolved[key]
		}
	}
	return nil
}

// resolveGit reads which commit a Git source's ref names now.
func (e *Engine) resolveGit(ctx context.Context, source model.GitSource) model.GitSource {
	ctx, cancel := context.WithTimeout(ctx, gitResolveTimeout)
	defer cancel()
	checkout, err := git.CloneCheckout(ctx, e.Repo.Executable, source.URL, source.Ref)
	source.ResolvedAt = e.now()
	switch {
	case errors.Is(err, git.ErrNoRef) && source.Ref == "":
		source.Unresolved = source.URL + " names no default branch"
	case errors.Is(err, git.ErrNoRef):
		source.Unresolved = fmt.Sprintf("%s has no branch or tag %s", source.URL, source.Ref)
	case err != nil:
		source.Unresolved = "its refs couldn't be read: " + err.Error()
	default:
		source.Commit, source.Abbreviation = model.ObjectID(checkout.Commit), checkout.Abbreviation
	}
	return source
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

// planVariants gives the one port a check selects the variant builds it
// asks for (Design v3 §3): with --variants, that port built with them in
// place of its defaults; with --variants each, beside its default build,
// one for each variant it declares (VariantBuilds). Each is evaluated
// with its variants in each environment, since what planning reads of a
// port can change with them, and is left out where the port doesn't
// declare them. A variant the port doesn't declare is refused.
func planVariants(ctx context.Context, reader PortReader, source model.Source, request PlanRequest, plan *model.Plan, candidates []model.PlanTarget, evaluations []planning.Evaluation, infos []map[model.TargetID]macports.PortInfo) ([]model.PlanTarget, error) {
	selected, err := variantsTarget(candidates, request.Only)
	if err != nil {
		return candidates, err
	}
	declared := make([][]macports.Variant, len(plan.Environments))
	var all []macports.Variant
	for e := range plan.Environments {
		info, ok := infos[e][selected.ID]
		if !ok {
			continue
		}
		if declared[e], err = info.Variants(); err != nil {
			return candidates, err
		}
		for _, variant := range declared[e] {
			if !slices.ContainsFunc(all, func(v macports.Variant) bool { return v.Name == variant.Name }) {
				all = append(all, variant)
			}
		}
	}
	sets := []map[string]bool{request.Variants}
	if request.EachVariant {
		if sets = VariantBuilds(all); len(sets) == 0 {
			return candidates, fmt.Errorf("--variants each: %s declares no variant beyond its defaults and universal", selected.Target.Name)
		}
	} else if missing := macports.Undeclared(request.Variants, all); len(missing) > 0 {
		return candidates, fmt.Errorf("--variants: %s declares no %s", selected.Target.Name, strings.Join(missing, ", "))
	}
	var builds []model.PlanTarget
	for _, variants := range sets {
		target := selected
		target.Target.Variants = variants
		target.ID = target.Target.ID()
		for e, environment := range plan.Environments {
			if len(macports.Undeclared(variants, declared[e])) > 0 {
				continue // not there: the build would be its defaults'
			}
			ports, err := reader.Ports(ctx, source, selected.Directory, environment, variants)
			if err != nil {
				plan.Unresolved = append(plan.Unresolved, model.Unresolved{Target: target.Target, Reason: err.Error()})
				continue
			}
			i := slices.IndexFunc(ports, func(port macports.PortInfo) bool { return port.Name == selected.Target.Name })
			if i < 0 {
				plan.Unresolved = append(plan.Unresolved, model.Unresolved{Target: target.Target, Reason: "with " + target.Target.VariantSpec() + " it isn't defined"})
				continue
			}
			evaluated, err := planning.Evaluate(ports[i], environment.Platform)
			if err != nil {
				plan.Unresolved = append(plan.Unresolved, model.Unresolved{Target: target.Target, Reason: err.Error()})
				continue
			}
			evaluations[e][target.ID] = evaluated
		}
		builds = append(builds, target)
	}
	at := slices.IndexFunc(candidates, func(c model.PlanTarget) bool { return c.ID == selected.ID })
	if !request.EachVariant {
		// Built with the variants instead of its defaults.
		for e := range evaluations {
			delete(evaluations[e], selected.ID)
		}
		return slices.Replace(candidates, at, at+1, builds...), nil
	}
	return slices.Insert(candidates, at+1, builds...), nil
}

// variantsTarget is the one port --variants builds: the one --only names,
// or the one port the branch changes.
func variantsTarget(candidates []model.PlanTarget, only []string) (model.PlanTarget, error) {
	switch {
	case len(only) == 1:
		if i := slices.IndexFunc(candidates, func(c model.PlanTarget) bool { return c.Target.Name == only[0] }); i >= 0 {
			return candidates[i], nil
		}
		return model.PlanTarget{}, fmt.Errorf("--only %s: the branch does not change it", only[0])
	case len(only) > 1:
		return model.PlanTarget{}, fmt.Errorf("--variants builds one port; --only names %d", len(only))
	}
	var changed []model.PlanTarget
	for _, c := range candidates {
		if c.Role == model.Changed {
			changed = append(changed, c)
		}
	}
	if len(changed) != 1 {
		return model.PlanTarget{}, fmt.Errorf("--variants builds one port, and the branch changes %d; name it with --only", len(changed))
	}
	return changed[0], nil
}

// VariantBuilds are the builds --variants each makes of a port beside its
// defaults: one for each variant it declares, over its defaults, but for
// the ones among its defaults, which its default build has, and
// universal, which needs other architectures' dependencies a clean
// builder doesn't have.
func VariantBuilds(declared []macports.Variant) []map[string]bool {
	var builds []map[string]bool
	for _, variant := range declared {
		if variant.Default || variant.Name == "universal" {
			continue
		}
		builds = append(builds, map[string]bool{variant.Name: true})
	}
	return builds
}

// VariantsFlag reads check's --variants: "each", or variants as MacPorts'
// command line takes them (macports.ParseVariants); none where empty.
func VariantsFlag(value string) (map[string]bool, bool, error) {
	switch strings.TrimSpace(value) {
	case "":
		return nil, false, nil
	case "each":
		return nil, true, nil
	}
	variants, err := macports.ParseVariants(value)
	if err != nil {
		return nil, false, fmt.Errorf("--variants: %w", err)
	}
	return variants, false, nil
}

// dependencyGraphReader is a port reader that can read every port's direct
// dependencies from the revision's port index.
type dependencyGraphReader interface {
	DependencyGraph(ctx context.Context, source model.Source) (map[string][]string, error)
}

// throughUnchanged adds to each candidate's evaluated dependencies the
// candidates it depends on only by way of ports the plan doesn't build:
// py313-mlx-vlm on py313-safetensors through py313-transformers, which
// --only had put first (field testing's py-mlx-vlm, check-201). A guest
// resolving from the branch's tree builds the changed one as it goes, but
// where the port between comes from a published archive, the order is
// what makes the build use the branch's. Read from the port index, once,
// and only where there are two candidates to order; where it can't be
// read, the plan orders by direct dependencies alone, as before.
func throughUnchanged(ctx context.Context, reader PortReader, source model.Source, candidates []model.PlanTarget, evaluations []planning.Evaluation) {
	graphs, ok := reader.(dependencyGraphReader)
	if !ok || len(candidates) < 2 {
		return
	}
	graph, err := graphs.DependencyGraph(ctx, source)
	if err != nil {
		progress.VerboseReport(ctx, "ordering by direct dependencies alone: %v", err)
		return
	}
	byName := map[string]model.TargetID{}
	for _, candidate := range candidates {
		// A port's default build, not one of --variants' builds of it.
		name := candidate.Target.Name
		if candidate.Target.Subport != "" {
			name = candidate.Target.Subport
		}
		if model.TargetID(name) == candidate.ID {
			byName[strings.ToLower(name)] = candidate.ID
		}
	}
	for _, evaluation := range evaluations {
		for id, evaluated := range evaluation {
			seen := map[string]bool{}
			var queue []string
			for _, dependency := range evaluated.Dependencies {
				if _, candidate := byName[strings.ToLower(string(dependency.Port))]; !candidate {
					queue = append(queue, strings.ToLower(string(dependency.Port)))
				}
			}
			for len(queue) > 0 {
				name := queue[0]
				queue = queue[1:]
				if seen[name] {
					continue
				}
				seen[name] = true
				for _, next := range graph[name] {
					if reached, candidate := byName[next]; candidate {
						if reached != id && !slices.ContainsFunc(evaluated.Dependencies, func(d planning.Dependency) bool { return d.Port == reached }) {
							evaluated.Dependencies = append(evaluated.Dependencies, planning.Dependency{Port: reached})
						}
						continue
					}
					queue = append(queue, next)
				}
			}
			evaluation[id] = evaluated
		}
	}
}
