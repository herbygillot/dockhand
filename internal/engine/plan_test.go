package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/buildenv"
	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
	"github.com/herbygillot/dockhand/internal/testsupport"
)

// fakePorts stands in for MacPorts' evaluator: each directory's ports, and
// what each depends on.
type fakePorts struct {
	directories map[string][]macports.PortInfo
	broken      map[string]bool
	// withVariants is a port as its variants make it, where they change
	// what planning reads.
	withVariants func(port macports.PortInfo, variants map[string]bool) macports.PortInfo
	// trees are directories as a tree has them, where it has them
	// otherwise than directories does.
	trees map[model.ObjectID]map[string][]macports.PortInfo
}

func (f fakePorts) Ports(_ context.Context, source model.Source, directory string, environment model.Environment, variants map[string]bool) ([]macports.PortInfo, error) {
	if f.broken[directory] {
		return nil, errors.New("Portfile error: can't read \"foo\": no such variable")
	}
	ports, ok := f.directories[directory]
	if tree, has := f.trees[source.Tree][directory]; has {
		ports, ok = tree, true
	}
	if !ok {
		return nil, errors.New("no Portfile")
	}
	if len(variants) > 0 && f.withVariants != nil {
		ports = slices.Clone(ports)
		for i := range ports {
			ports[i] = f.withVariants(ports[i], variants)
		}
	}
	return ports, nil
}

func (f fakePorts) Directory(_ context.Context, _ model.Source, name string) (string, error) {
	for directory, ports := range f.directories {
		for _, port := range ports {
			if port.Name == name {
				return directory, nil
			}
		}
	}
	return "", notInTree(name)
}

func port(name string, deps ...string) macports.PortInfo {
	info := macports.PortInfo{Name: name, Options: map[string]string{}}
	for _, dep := range deps {
		info.Dependencies = append(info.Dependencies, macports.Dependency{Port: dep, Phase: "lib"})
	}
	return info
}

var (
	tahoeArm = model.Environment{Provider: "command", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}}
	tahoeX86 = model.Environment{Provider: "command", Platform: model.Platform{OS: "darwin", Version: "25", Architecture: "x86_64"}}
)

func names(targets []model.PlanTarget) []string {
	var all []string
	for _, target := range targets {
		all = append(all, string(target.ID)+":"+string(target.Kind)+":"+string(target.Role))
	}
	return all
}

// harborBranch changes libharbor substantively, harbor-cli by revision
// only, and harbor-viewer under files/.
func harborBranch(t *testing.T, e *Engine) model.Revision {
	t.Helper()
	branch, err := e.Start(t.Context(), StartRequest{Name: "libharbor-2", Here: true})
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{
		"devel/libharbor/Portfile":             "name libharbor\nversion 3\n",
		"devel/harbor-cli/Portfile":            "name harbor-cli\nrevision 0\n",
		"graphics/harbor-viewer/Portfile":      "name harbor-viewer\n",
		"graphics/harbor-viewer/files/a.patch": "a\n",
		"graphics/harbor-tools/Portfile":       "name harbor-tools\n",
	})
	testsupport.Git(t, branch.Worktree, "add", "-A")
	testsupport.Git(t, branch.Worktree, "commit", "-q", "-m", "base for the ports")
	base := testsupport.Git(t, branch.Worktree, "rev-parse", "HEAD")
	write(t, branch.Worktree, map[string]string{
		"devel/libharbor/Portfile":             "name libharbor\nversion 4\n",
		"devel/harbor-cli/Portfile":            "name harbor-cli\nrevision 1\n",
		"graphics/harbor-viewer/files/a.patch": "b\n",
	})
	branch.Base = model.ObjectID(base)
	capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch})
	require.NoError(t, err)
	return capture.Revision
}

func harborPorts() fakePorts {
	legacy := port("harbor-viewer-legacy", "libharbor")
	legacy.Options["supported_archs"] = "x86_64"
	old := port("harbor-cli-old")
	old.Options["replaced_by"] = "harbor-cli"
	return fakePorts{directories: map[string][]macports.PortInfo{
		"devel/libharbor":        {port("libharbor")},
		"devel/harbor-cli":       {port("harbor-cli", "libharbor"), old},
		"graphics/harbor-viewer": {port("harbor-viewer", "libharbor", "harbor-cli"), legacy},
		"graphics/harbor-tools":  {port("harbor-tools", "harbor-viewer")},
	}}
}

func TestPlanFollowsCIsScopeOrderAndEligibility(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	revision := harborBranch(t, e)
	e.PortReader = harborPorts()

	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm, tahoeX86}, Also: []string{"harbor-tools"}})
	require.NoError(t, err)
	require.True(t, plan.Runnable())
	require.Equal(t, []string{
		"libharbor:substantive:changed",
		"harbor-cli:revision-only:changed",
		"harbor-viewer:substantive:changed",
		"harbor-viewer-legacy:substantive:changed",
		"harbor-tools:unchanged:also",
	}, names(plan.Targets), "dependencies first, subports kept, replaced ports gone")
	for _, environment := range []model.Environment{tahoeArm, tahoeX86} {
		require.Equal(t, []model.TargetID{"libharbor", "harbor-cli"}, plan.DependsOnIn(environment, "harbor-viewer"))
	}
	require.Equal(t, "harbor-viewer-legacy", plan.Targets[3].Target.Subport)
	armPlan, _ := plan.In(tahoeArm)
	x86Plan, _ := plan.In(tahoeX86)
	require.Equal(t, []model.Exclusion{
		{Target: model.Target{Name: "harbor-cli-old", Portfile: "devel/harbor-cli/Portfile", Subport: "harbor-cli-old"}, Reason: "replaced by harbor-cli"},
		{Target: model.Target{Name: "harbor-viewer-legacy", Portfile: "graphics/harbor-viewer/Portfile", Subport: "harbor-viewer-legacy"}, Reason: "supported_archs x86_64 only"},
	}, armPlan.Exclusions)
	require.Equal(t, []model.Exclusion{
		{Target: model.Target{Name: "harbor-cli-old", Portfile: "devel/harbor-cli/Portfile", Subport: "harbor-cli-old"}, Reason: "replaced by harbor-cli"},
	}, x86Plan.Exclusions, "a port excluded everywhere is excluded in each environment, and not planned")
	require.True(t, plan.Excludes(plan.Targets[3], tahoeArm), "x86_64 only")
	require.False(t, plan.Excludes(plan.Targets[3], tahoeX86))

	narrowed, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}, Only: []string{"harbor-viewer"}})
	require.NoError(t, err)
	require.Equal(t, []string{
		"libharbor:substantive:prerequisite",
		"harbor-cli:revision-only:prerequisite",
		"harbor-viewer:substantive:changed",
	}, names(narrowed.Targets), "changed prerequisites come back, visibly")
	_, err = e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}, Only: []string{"harbor-tools"}})
	require.ErrorContains(t, err, "--only harbor-tools: the branch does not change it")
}

func TestAPlanThatCannotEvaluateIsUnresolved(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	revision := harborBranch(t, e)
	ports := harborPorts()
	ports.broken = map[string]bool{"graphics/harbor-viewer": true}
	e.PortReader = ports
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}})
	require.NoError(t, err)
	require.False(t, plan.Runnable())
	require.Len(t, plan.Unresolved, 1)
	require.Equal(t, "harbor-viewer", plan.Unresolved[0].Target.Name)
	require.Contains(t, plan.Unresolved[0].Reason, "no such variable")

	cyclic := harborPorts()
	cyclic.directories["devel/libharbor"] = []macports.PortInfo{port("libharbor", "harbor-cli")}
	e.PortReader = cyclic
	plan, err = e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}})
	require.NoError(t, err)
	require.False(t, plan.Runnable())
	require.Equal(t, "dependency cycle on command macOS 26 (Tahoe) arm64: harbor-cli → libharbor → harbor-cli", plan.Unresolved[0].Reason)
}

// A revision bump of a port whose shared code changed on the branch is
// substantive, so a failure the shared code causes can't be accepted as
// "cause not established" (Design v3 §3: revision-only "shared code
// included"). From the 2026-09-25 implementation review. The source
// settles who loads a PortGroup, through other PortGroups too; what it
// can't settle counts as substantive.
func TestChangedSharedCodeIsSubstantive(t *testing.T) {
	f := setup(t)
	write(t, f.upstream, map[string]string{
		"textproc/jq/Portfile":                           "PortGroup github 1.0\nname jq\nversion 1.7.1\nrevision 0\n",
		"_resources/port1.0/group/github-1.0.tcl":        "PortGroup legacysupport 1.1\n",
		"_resources/port1.0/group/legacysupport-1.1.tcl": "# legacy support\n",
	})
	testsupport.Git(t, f.upstream, "add", "-A")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "jq: load group")
	e := f.open(t)
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}}}
	for i, c := range []struct {
		why   string
		files map[string]string
		kind  model.TargetKind
	}{
		{"a revision bump alone", nil, model.RevisionOnly},
		{"a PortGroup jq doesn't load", map[string]string{"_resources/port1.0/group/qt5-1.0.tcl": "# changed\n"}, model.RevisionOnly},
		{"a PortGroup jq loads", map[string]string{"_resources/port1.0/group/github-1.0.tcl": "PortGroup legacysupport 1.1\npost-destroot { error {fails here} }\n"}, model.Substantive},
		{"a PortGroup jq loads through another", map[string]string{"_resources/port1.0/group/legacysupport-1.1.tcl": "# changed\n"}, model.Substantive},
		{"shared code Base reads for every port", map[string]string{"_resources/port1.0/compilers/clang_compilers.tcl": "# changed\n"}, model.Substantive},
		{"a PortGroup line the source doesn't spell", map[string]string{
			"textproc/jq/Portfile":                 "PortGroup ${group} 1.0\nname jq\nversion 1.7.1\nrevision 1\n",
			"_resources/port1.0/group/qt5-1.0.tcl": "# changed\n",
		}, model.Substantive},
	} {
		branch, err := e.Start(t.Context(), StartRequest{Name: fmt.Sprintf("shared-%d", i)})
		require.NoError(t, err)
		testsupport.Git(t, branch.Worktree, "sparse-checkout", "add", "_resources")
		files := map[string]string{"textproc/jq/Portfile": "PortGroup github 1.0\nname jq\nversion 1.7.1\nrevision 1\n"}
		for path, text := range c.files {
			files[path] = text
		}
		write(t, branch.Worktree, files)
		testsupport.Git(t, branch.Worktree, "add", "-A") // new files are captured once tracked
		capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch})
		require.NoError(t, err)
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}})
		require.NoError(t, err)
		require.Len(t, plan.Targets, 1, c.why)
		require.Equal(t, c.kind, plan.Targets[0].Kind, c.why)
		require.Equal(t, c.kind == model.RevisionOnly, TargetEvidence{Target: plan.Targets[0]}.Acceptable(), c.why)
	}
}

// harborByPlatform is harborPorts, with harbor-viewer linking libharbor
// only on x86_64, and, when crossed, libharbor and harbor-cli needing
// each other on different platforms.
type harborByPlatform struct {
	fakePorts
	crossed bool
}

func (p harborByPlatform) Ports(ctx context.Context, source model.Source, directory string, environment model.Environment, variants map[string]bool) ([]macports.PortInfo, error) {
	x86 := environment.Platform.Architecture == "x86_64"
	switch {
	case directory == "graphics/harbor-viewer" && !x86:
		return []macports.PortInfo{port("harbor-viewer")}, nil
	case directory == "graphics/harbor-viewer":
		return []macports.PortInfo{port("harbor-viewer", "libharbor")}, nil
	case p.crossed && directory == "devel/libharbor" && !x86:
		return []macports.PortInfo{port("libharbor", "harbor-cli")}, nil
	case p.crossed && directory == "devel/harbor-cli" && x86:
		return []macports.PortInfo{port("harbor-cli", "libharbor")}, nil
	case p.crossed && directory == "devel/harbor-cli":
		return []macports.PortInfo{port("harbor-cli")}, nil
	}
	return p.fakePorts.Ports(ctx, source, directory, environment, variants)
}

// Each platform keeps its own dependencies: --only adds back a changed
// prerequisite any platform needs, whichever was evaluated first, and a
// guest blocks a target only on what it needs on its own platform. From
// the 2026-09-25 implementation review.
func TestEachPlatformKeepsItsOwnDependencies(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	revision := harborBranch(t, e)
	e.PortReader = harborByPlatform{fakePorts: harborPorts()}

	for _, environments := range [][]model.Environment{{tahoeArm, tahoeX86}, {tahoeX86, tahoeArm}} {
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: environments, Only: []string{"harbor-viewer"}})
		require.NoError(t, err)
		library, included := plan.Target("libharbor")
		require.True(t, included, "x86_64 needs the changed library, whichever platform comes first")
		require.Equal(t, model.Prerequisite, library.Role)
		require.Empty(t, plan.DependsOnIn(tahoeArm, "harbor-viewer"))
		require.Equal(t, []model.TargetID{"libharbor"}, plan.DependsOnIn(tahoeX86, "harbor-viewer"))
	}

	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm, tahoeX86}})
	require.NoError(t, err)
	var branch model.Branch
	require.NoError(t, e.Store.View(t.Context(), e.Repository, func(r store.Reader) error {
		branch, err = r.Branch(revision.Branch)
		return err
	}))
	provider := &scriptedProvider{outcomes: map[model.TargetID]model.Outcome{"libharbor": model.OutcomeFailed}}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	got := outcomes(t, e, run)
	require.Equal(t, model.OutcomePassed, got["harbor-viewer@arm64"], "arm64's harbor-viewer doesn't link libharbor, so its failure doesn't block it")
	require.Equal(t, model.OutcomeBlocked, got["harbor-viewer@x86_64"])
	for _, job := range provider.jobs {
		i := slices.IndexFunc(job.Targets, func(target buildenv.Target) bool { return target.ID == "harbor-viewer" })
		require.GreaterOrEqual(t, i, 0)
		require.Equal(t, plan.DependsOnIn(job.Environment, "harbor-viewer"), job.Targets[i].DependsOn, "a provider sees its own platform's dependencies")
	}

	// Dependencies that run opposite ways on two platforms are no cycle:
	// each builds in its own order.
	e.PortReader = harborByPlatform{fakePorts: harborPorts(), crossed: true}
	crossed, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm, tahoeX86}})
	require.NoError(t, err)
	require.Empty(t, crossed.Unresolved)
	armPlan, _ := crossed.In(tahoeArm)
	x86Plan, _ := crossed.In(tahoeX86)
	require.Equal(t, []model.TargetID{"harbor-cli", "libharbor", "harbor-viewer"}, slices.DeleteFunc(slices.Clone(armPlan.Order), func(id model.TargetID) bool { return id == "harbor-tools" }))
	require.Equal(t, []model.TargetID{"libharbor", "harbor-cli", "harbor-viewer"}, slices.DeleteFunc(slices.Clone(x86Plan.Order), func(id model.TargetID) bool { return id == "harbor-tools" }))
	crossed.ID, crossed.Revision = model.PlanID(store.NewID("plan")), revision.ID
	provider = &scriptedProvider{}
	e.Providers = map[string]buildenv.Provider{"command": provider}
	queued, err = e.Enqueue(t.Context(), branch, crossed, model.OriginPerson)
	require.NoError(t, err)
	run, err = e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	require.Equal(t, model.RunPassed, run.State)
	for _, job := range provider.jobs {
		var order []model.TargetID
		for _, target := range job.Targets {
			order = append(order, target.ID)
		}
		planned, _ := crossed.In(job.Environment)
		require.Equal(t, planned.Order, order, "each provider builds in its own environment's order")
	}
}

// armOnlyViewer defines an Intel subport of harbor-viewer only where the
// evaluation is on x86_64, as a Portfile's platform conditions can.
type armOnlyViewer struct{ fakePorts }

func (p armOnlyViewer) Ports(ctx context.Context, source model.Source, directory string, environment model.Environment, variants map[string]bool) ([]macports.PortInfo, error) {
	ports, err := p.fakePorts.Ports(ctx, source, directory, environment, variants)
	if err == nil && directory == "graphics/harbor-viewer" {
		ports = []macports.PortInfo{port("harbor-viewer")}
		if environment.Platform.Architecture == "x86_64" {
			ports = append(ports, port("harbor-viewer-intel"))
		}
	}
	return ports, err
}

// A port one environment's evaluation didn't define isn't built there,
// whichever environment defined it. (The architecture review of
// 2026-09-27, finding 2.)
func TestAPortAnEnvironmentDoesNotDefineIsNotBuiltThere(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	revision := harborBranch(t, e)
	e.PortReader = armOnlyViewer{harborPorts()}
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm, tahoeX86}})
	require.NoError(t, err)
	target, ok := plan.Target("harbor-viewer-intel")
	require.True(t, ok, "built where it is defined")
	require.True(t, plan.Excludes(target, tahoeArm), "not where it isn't")
	require.False(t, plan.Excludes(target, tahoeX86))
	exclusion, _ := plan.ExclusionIn(tahoeArm, "harbor-viewer-intel")
	require.Equal(t, "not defined there", exclusion.Reason)
}

// A plan records the targets that declare no tests, by test.run as
// MacPorts reads it, so a check requiring tests can say it asks
// nothing of them (the ov run's finding 3). One whose test.run wasn't read
// is left unsaid.
func TestAPlanRecordsWhatDeclaresNoTests(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	revision := harborBranch(t, e)
	ports := harborPorts()
	ports.directories["devel/libharbor"][0].Options["dockhand.test_run"] = "1"
	ports.directories["devel/harbor-cli"][0].Options["dockhand.test_run"] = "0"
	e.PortReader = ports

	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}, Tests: model.TestsRequired})
	require.NoError(t, err)
	planned, _ := plan.In(tahoeArm)
	require.Equal(t, []model.TargetID{"harbor-cli"}, planned.Untested, "libharbor declares tests, and harbor-viewer's test.run wasn't read")

	require.Equal(t, "✓ declares no tests", targetWords(plan, model.PlanTarget{ID: "harbor-cli"}, recorded(tahoeArm, model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsNone}), "", false))
	plan.Tests = model.TestsDeclared
	require.Equal(t, "✓", targetWords(plan, model.PlanTarget{ID: "harbor-cli"}, recorded(tahoeArm, model.TargetResult{Outcome: model.OutcomePassed, Tests: model.TestsNone}), "", false), "only a policy requiring tests needs saying so")
}

// A port whose eligibility couldn't be read is unresolved, neither built
// nor excluded; one excluded by its platforms is named for them.
func TestAPlanKeepsAnUnreadEligibilityApart(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	revision := harborBranch(t, e)
	ports := harborPorts()
	ports.directories["devel/libharbor"][0].OptionErrors = map[string]string{"supported_archs": "cannot evaluate"}
	e.PortReader = ports
	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}})
	require.NoError(t, err)
	require.Contains(t, plan.Unresolved, model.Unresolved{Target: model.Target{Name: "libharbor", Portfile: "devel/libharbor/Portfile"}, Reason: "macports: evaluating supported_archs: cannot evaluate"})

	ports = harborPorts()
	cli := ports.directories["devel/harbor-cli"][0]
	cli.Options["dockhand.known_fail"], cli.Options["dockhand.platforms_compatible"], cli.Options["platforms"] = "1", "0", "{darwin >= 26}"
	e.PortReader = ports
	plan, err = e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm}})
	require.NoError(t, err)
	exclusion, excluded := plan.ExclusionIn(tahoeArm, "harbor-cli")
	require.True(t, excluded)
	require.Equal(t, "its platforms, {darwin >= 26}, exclude this release", exclusion.Reason)
}

// harborVariants gives libharbor variants: +tests declares its tests, and
// +docs needs Xcode, as what planning reads can change with a variant;
// x11 is a default, and universal is Base's own.
func harborVariants() fakePorts {
	ports := harborPorts()
	lib := port("libharbor")
	lib.Options["dockhand.test_run"] = "0"
	lib.Options["variants"] = "tests docs x11 universal"
	lib.Options["vinfo"] = "tests {description Tests} docs {requires tests} x11 {is_default +} universal {}"
	ports.directories["devel/libharbor"] = []macports.PortInfo{lib}
	ports.withVariants = func(port macports.PortInfo, variants map[string]bool) macports.PortInfo {
		port.Options = maps.Clone(port.Options)
		if variants["tests"] || variants["docs"] { // docs requires tests, which MacPorts turns on with it
			port.Options["dockhand.test_run"] = "1"
		}
		if variants["docs"] {
			port.Options["use_xcode"] = "yes"
		}
		return port
	}
	return ports
}

// x86LacksDocs is libharbor without +docs on x86_64.
type x86LacksDocs struct{ fakePorts }

func (p x86LacksDocs) Ports(ctx context.Context, source model.Source, directory string, environment model.Environment, variants map[string]bool) ([]macports.PortInfo, error) {
	ports, err := p.fakePorts.Ports(ctx, source, directory, environment, variants)
	if err == nil && environment == tahoeX86 && directory == "devel/libharbor" {
		ports = slices.Clone(ports)
		ports[0].Options = maps.Clone(ports[0].Options)
		ports[0].Options["variants"] = "tests x11 universal"
	}
	return ports, err
}

// --variants builds the one port a check selects its way, evaluated with
// the variants, since what planning reads can change with them: in place
// of its defaults, or, with each, beside them, one build for each variant
// it declares but its defaults and universal. A variant it doesn't
// declare there leaves that build out there; one it declares nowhere, a
// check of more than one port, and GitHub's workflow, which builds
// default variants alone, are refused (item 8).
func TestVariantsBuildOnePortTheirWay(t *testing.T) {
	f := setup(t)
	e := f.open(t)
	revision := harborBranch(t, e)
	e.PortReader = x86LacksDocs{harborVariants()}
	both := []model.Environment{tahoeArm, tahoeX86}

	plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: both, Only: []string{"libharbor"}, Variants: map[string]bool{"tests": true}})
	require.NoError(t, err)
	require.Equal(t, []string{"libharbor +tests:substantive:changed"}, names(plan.Targets), "in place of its defaults")
	require.Equal(t, "+tests", plan.Variants)
	arm, _ := plan.In(tahoeArm)
	require.Empty(t, arm.Untested, "+tests declares its tests")

	plan, err = e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: both, Only: []string{"libharbor"}, EachVariant: true})
	require.NoError(t, err)
	require.True(t, plan.EachVariant)
	require.Equal(t, []string{"libharbor:substantive:changed", "libharbor +tests:substantive:changed", "libharbor +docs:substantive:changed"}, names(plan.Targets),
		"x11 is among its defaults, and universal is left out")
	arm, _ = plan.In(tahoeArm)
	require.Equal(t, []model.TargetID{"libharbor"}, arm.Untested)
	require.Equal(t, []model.TargetID{"libharbor +docs"}, arm.NeedsXcode)
	x86, _ := plan.In(tahoeX86)
	require.NotContains(t, x86.Order, model.TargetID("libharbor +docs"))
	exclusion, excluded := plan.ExclusionIn(tahoeX86, "libharbor +docs")
	require.True(t, excluded)
	require.Equal(t, "not defined there", exclusion.Reason)
	require.False(t, plan.Excludes(plan.Targets[0], tahoeX86), "the port's other builds aren't excluded with it")
	require.Equal(t, []string{"harbor-cli:revision-only:changed", "harbor-viewer:substantive:changed", "harbor-viewer-legacy:substantive:changed"}, names(plan.Omitted))

	_, err = e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: both, Only: []string{"libharbor"}, Variants: map[string]bool{"gui": true}})
	require.ErrorContains(t, err, "--variants: libharbor declares no gui")
	_, err = e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: both, Variants: map[string]bool{"tests": true}})
	require.ErrorContains(t, err, "--variants builds one port, and the branch changes 5; name it with --only")
	_, err = e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{{Provider: "github"}}, Only: []string{"libharbor"}, EachVariant: true})
	require.ErrorContains(t, err, "GitHub's workflow builds default variants only")
	_, err = e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: both, Only: []string{"libharbor"}, EachVariant: true, Variants: map[string]bool{"tests": true}})
	require.ErrorContains(t, err, "--variants each builds every variant")
}
