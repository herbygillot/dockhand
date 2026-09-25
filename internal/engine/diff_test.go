package engine

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/model"
)

// Dependents reads the fake's dependencies backwards, as the port index
// would.
func (f fakePorts) Dependents(_ context.Context, _ model.Source, directories []string) ([]Dependent, error) {
	phases := map[string]string{"lib": "library", "build": "build", "run": "runtime"}
	var changed []string
	for _, directory := range directories {
		for _, port := range f.directories[directory] {
			changed = append(changed, port.Name)
		}
	}
	var all []Dependent
	for directory, ports := range f.directories {
		if slices.Contains(directories, directory) {
			continue
		}
		for _, port := range ports {
			dependent := Dependent{Name: port.Name, Directory: directory}
			for _, dependency := range port.Dependencies {
				if slices.Contains(changed, dependency.Port) {
					dependent.On = append(dependent.On, dependency.Port)
					dependent.Phases = append(dependent.Phases, phases[dependency.Phase])
				}
			}
			if len(dependent.On) > 0 {
				all = append(all, dependent)
			}
		}
	}
	slices.SortFunc(all, func(a, b Dependent) int { return strings.Compare(a.Name, b.Name) })
	return all, nil
}

// harborMaster puts the harbor ports on master, with one port that loads
// the github PortGroup.
func harborMaster(t *testing.T, f fixture) {
	t.Helper()
	write(t, f.upstream, map[string]string{
		"devel/harbor-cli/Portfile":            "name harbor-cli\nrevision 0\n",
		"graphics/harbor-viewer/Portfile":      "name harbor-viewer\n",
		"graphics/harbor-viewer/files/a.patch": "a\n",
		"graphics/harbor-tools/Portfile":       "name harbor-tools\n",
		"net/harbor-sync/Portfile":             "PortGroup           github 1.0\nname harbor-sync\n",
	})
	run(t, f.upstream, "add", "-A")
	run(t, f.upstream, "commit", "-q", "-m", "the harbor ports")
}

func TestDiffShowsTheBranchAsItIsNow(t *testing.T) {
	f := setup(t)
	harborMaster(t, f)
	e := f.open(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "libharbor-3", Here: true})
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{"devel/libharbor/Portfile": "name libharbor\nversion 3\n"})
	commitAs(t, branch.Worktree, "Ada ada@example.org", "libharbor: update to 3")
	write(t, branch.Worktree, map[string]string{
		"devel/harbor-cli/Portfile":               "name harbor-cli\nrevision 1\n",
		"textproc/jq/README":                      "not built\n",
		"_resources/port1.0/group/github-1.0.tcl": "# group, changed\n",
	})
	run(t, branch.Worktree, "add", "textproc/jq/README")

	diff, err := e.Diff(t.Context(), branch, nil)
	require.NoError(t, err)
	require.Equal(t, []PortDiff{
		{Directory: "devel/harbor-cli", Kind: model.RevisionOnly},
		{Directory: "devel/libharbor", Kind: model.Substantive},
	}, diff.Ports)
	require.Equal(t, []string{"_resources/port1.0/group/github-1.0.tcl", "textproc/jq/README"}, diff.Other)
	require.Contains(t, string(diff.Patch), "-revision 0\n+revision 1", "uncommitted edits are in it")
	require.Contains(t, string(diff.Patch), "+version 3", "and so are commits")

	narrowed, err := e.Diff(t.Context(), branch, []string{"devel/libharbor"})
	require.NoError(t, err)
	require.Equal(t, []string{"devel/libharbor/Portfile"}, narrowed.Files)
	require.NotContains(t, string(narrowed.Patch), "harbor-cli")
}

func TestImpactNamesDependentsAndSharedFiles(t *testing.T) {
	f := setup(t)
	harborMaster(t, f)
	e := f.open(t)
	e.PortReader = harborPorts()
	branch, err := e.Start(t.Context(), StartRequest{Name: "libharbor-3", Here: true})
	require.NoError(t, err)
	write(t, branch.Worktree, map[string]string{
		"devel/libharbor/Portfile":                "name libharbor\nversion 3\n",
		"devel/harbor-cli/Portfile":               "name harbor-cli\nrevision 1\n",
		"_resources/port1.0/group/github-1.0.tcl": "# group, changed\n",
	})

	impact, err := e.Impact(t.Context(), branch, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"devel/libharbor"}, impact.Of, "a revision bump's dependents are not in question")
	require.Equal(t, []Dependent{
		{Name: "harbor-viewer", Directory: "graphics/harbor-viewer", On: []string{"libharbor"}, Phases: []string{"library"}},
		{Name: "harbor-viewer-legacy", Directory: "graphics/harbor-viewer", On: []string{"libharbor"}, Phases: []string{"library"}},
	}, impact.Dependents, "harbor-cli is changed, so it is not another dependent")
	require.Equal(t, []SharedFile{{Path: "_resources/port1.0/group/github-1.0.tcl", PortGroup: "github 1.0", Users: []string{"net/harbor-sync"}}}, impact.Shared)

	named, err := e.Impact(t.Context(), branch, []string{"harbor-viewer"})
	require.NoError(t, err)
	require.Equal(t, []string{"graphics/harbor-viewer"}, named.Of, "an unchanged port can be asked about")
	require.Equal(t, []string{"harbor-tools"}, dependentNames(named.Dependents))
}

func dependentNames(dependents []Dependent) []string {
	var names []string
	for _, dependent := range dependents {
		names = append(names, dependent.Name)
	}
	return names
}

func TestLinkedPortsAreLibraryDependentsOncePerDirectory(t *testing.T) {
	f := setup(t)
	harborMaster(t, f)
	e := f.open(t)
	e.PortReader = harborPorts()
	branch, err := e.Start(t.Context(), StartRequest{Name: "libharbor-3", Here: true})
	require.NoError(t, err)

	linked, err := e.LinkedPorts(t.Context(), branch, "libharbor", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"harbor-cli", "harbor-viewer"}, dependentNames(linked.Bump), "harbor-viewer-legacy shares harbor-viewer's directory")

	write(t, branch.Worktree, map[string]string{"devel/harbor-cli/Portfile": "name harbor-cli\nrevision 1\n"})
	linked, err = e.LinkedPorts(t.Context(), branch, "libharbor", []string{"harbor-viewer"})
	require.NoError(t, err)
	require.Empty(t, linked.Bump)
	require.Equal(t, []string{"harbor-cli"}, dependentNames(linked.Changed))
	require.Equal(t, []string{"harbor-viewer"}, linked.Excepted)
	_, err = e.LinkedPorts(t.Context(), branch, "libharbor", []string{"harbor-tools"})
	require.ErrorContains(t, err, "--except harbor-tools: it is not a library dependent of libharbor")
}

// update --revbump-dependents is one engine operation: it bumps each
// linked port with the subject tidy uses, and a plan bumps nothing.
func TestRevbumpLinkedBumpsEachLinkedPortForTidy(t *testing.T) {
	f := setup(t)
	write(t, f.upstream, map[string]string{"textproc/yq/Portfile": "name yq\nversion 1\n", "textproc/gojq/Portfile": "name gojq\nversion 1\n"})
	run(t, f.upstream, "add", "-A")
	run(t, f.upstream, "commit", "-q", "-m", "yq and gojq")
	e, _ := f.withPreparer(t)
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{
		"textproc/jq": {port("jq")}, "textproc/yq": {port("yq", "jq")}, "textproc/gojq": {port("gojq", "jq")},
	}}
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	update := Update{Port: "jq", After: PortVersion{Version: "1.8.1"}}

	planned, err := e.RevbumpLinked(t.Context(), branch, update, nil, true)
	require.NoError(t, err)
	require.Equal(t, []string{"gojq", "yq"}, dependentNames(planned.Bump))
	require.Empty(t, planned.Bumped)
	require.Equal(t, "rebuild for jq 1.8.1", planned.Subject)
	require.NoFileExists(t, filepath.Join(branch.Worktree, "textproc/yq/Portfile"), "a plan bumps nothing")

	done, err := e.RevbumpLinked(t.Context(), branch, update, []string{"gojq"}, false)
	require.NoError(t, err)
	require.Equal(t, []string{"yq"}, done.Bumped)
	require.Equal(t, []string{"gojq"}, done.Excepted)
	require.Equal(t, "name yq\nversion 1\nrevision 1\n", read(t, filepath.Join(branch.Worktree, "textproc/yq/Portfile")))
	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.Equal(t, "yq: rebuild for jq 1.8.1", plan.Groups[0].Subject())
}
