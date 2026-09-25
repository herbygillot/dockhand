package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/store"
)

// fakePorts stands in for MacPorts' evaluator: each directory's ports, and
// what each depends on.
type fakePorts struct {
	directories map[string][]macports.PortInfo
	broken      map[string]bool
}

func (f fakePorts) Ports(_ context.Context, _ model.Source, directory string, platform model.Platform) ([]macports.PortInfo, error) {
	if f.broken[directory] {
		return nil, errors.New("Portfile error: can't read \"foo\": no such variable")
	}
	ports, ok := f.directories[directory]
	if !ok {
		return nil, errors.New("no Portfile")
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
	return "", errors.New("no such port")
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
	run(t, branch.Worktree, "add", "-A")
	run(t, branch.Worktree, "commit", "-q", "-m", "base for the ports")
	base := run(t, branch.Worktree, "rev-parse", "HEAD")
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
	require.Equal(t, []model.TargetID{"libharbor", "harbor-cli"}, plan.Targets[2].DependsOn)
	require.Equal(t, "harbor-viewer-legacy", plan.Targets[3].Target.Subport)
	require.Len(t, plan.Exclusions, 3)
	require.True(t, Excluded(plan, plan.Targets[3], tahoeArm.Platform), "x86_64 only")
	require.False(t, Excluded(plan, plan.Targets[3], tahoeX86.Platform))

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
	require.Contains(t, plan.Unresolved[0].Reason, "dependency cycle: harbor-cli → libharbor → harbor-cli")
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
	run(t, f.upstream, "add", "-A")
	run(t, f.upstream, "commit", "-q", "-m", "jq: load group")
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
		run(t, branch.Worktree, "sparse-checkout", "add", "_resources")
		files := map[string]string{"textproc/jq/Portfile": "PortGroup github 1.0\nname jq\nversion 1.7.1\nrevision 1\n"}
		for path, text := range c.files {
			files[path] = text
		}
		write(t, branch.Worktree, files)
		run(t, branch.Worktree, "add", "-A") // new files are captured once tracked
		capture, err := e.Capture(t.Context(), CaptureRequest{Branch: branch})
		require.NoError(t, err)
		plan, err := e.PlanCheck(t.Context(), PlanRequest{Revision: capture.Revision, Environments: []model.Environment{tahoeArm}})
		require.NoError(t, err)
		require.Len(t, plan.Targets, 1, c.why)
		require.Equal(t, c.kind, plan.Targets[0].Kind, c.why)
		require.Equal(t, c.kind == model.RevisionOnly, Acceptable(plan.Targets[0]), c.why)
	}
}

// harborByPlatform is harborPorts, with harbor-viewer linking libharbor
// only on x86_64, and, when crossed, libharbor and harbor-cli needing
// each other on different platforms.
type harborByPlatform struct {
	fakePorts
	crossed bool
}

func (p harborByPlatform) Ports(ctx context.Context, source model.Source, directory string, platform model.Platform) ([]macports.PortInfo, error) {
	x86 := platform.Architecture == "x86_64"
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
	return p.fakePorts.Ports(ctx, source, directory, platform)
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
	e.Providers = map[string]Provider{"command": provider}
	queued, err := e.Enqueue(t.Context(), branch, plan, model.OriginPerson)
	require.NoError(t, err)
	run, err := e.Drive(t.Context(), session(t, e), queued.ID)
	require.NoError(t, err)
	got := outcomes(t, e, run)
	require.Equal(t, model.OutcomePassed, got["harbor-viewer@arm64"], "arm64's harbor-viewer doesn't link libharbor, so its failure doesn't block it")
	require.Equal(t, model.OutcomeBlocked, got["harbor-viewer@x86_64"])
	for _, job := range provider.jobs {
		i := slices.IndexFunc(job.Targets, func(target model.PlanTarget) bool { return target.ID == "harbor-viewer" })
		require.GreaterOrEqual(t, i, 0)
		require.Equal(t, plan.DependsOnIn(job.Environment, "harbor-viewer"), job.Targets[i].DependsOn, "a provider sees its own platform's dependencies")
	}

	e.PortReader = harborByPlatform{fakePorts: harborPorts(), crossed: true}
	crossed, err := e.PlanCheck(t.Context(), PlanRequest{Revision: revision, Environments: []model.Environment{tahoeArm, tahoeX86}})
	require.NoError(t, err)
	require.Len(t, crossed.Unresolved, 1)
	require.Contains(t, crossed.Unresolved[0].Reason, "dependency cycle across platforms, which no one platform has")
}
