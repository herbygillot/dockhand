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
	"github.com/herbygillot/dockhand/internal/testsupport"
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
	testsupport.Git(t, f.upstream, "add", "-A")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "the harbor ports")
}

func TestDiffShowsTheBranchAsItIsNow(t *testing.T) {
	t.Parallel()
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
	testsupport.Git(t, branch.Worktree, "add", "textproc/jq/README")

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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	f := setup(t)
	write(t, f.upstream, map[string]string{"textproc/yq/Portfile": "name yq\nversion 1\n", "textproc/gojq/Portfile": "name gojq\nversion 1\n"})
	testsupport.Git(t, f.upstream, "add", "-A")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "yq and gojq")
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

// Impact's dependents to build first are one of each kind there is,
// library first, a dependent of several kinds taken once: not the first
// three by name, aria2, bind9, and bind9.18, among the heaviest of
// libuv's to build (the libuv run's finding 3).
func TestImpactSuggestsOneDependentOfEachKind(t *testing.T) {
	t.Parallel()
	impact := Impact{Dependents: []Dependent{
		{Name: "aria2", Phases: []string{"build"}},
		{Name: "bind9", Phases: []string{"build"}},
		{Name: "luv", Phases: []string{"library", "runtime"}},
		{Name: "ttyd", Phases: []string{"library"}},
		{Name: "uvw", Phases: []string{"runtime"}},
	}}
	var names []string
	for _, dependent := range impact.OneOfEachKind() {
		names = append(names, dependent.Name)
	}
	require.Equal(t, []string{"luv", "aria2"}, names, "luv is the library and the runtime one")
	require.Empty(t, Impact{}.OneOfEachKind())
}

// A port that depends on a changed one only under a variant is a
// dependent too, which the index, recording default variants', doesn't
// name: enchant2 links nuspell only under +nuspell (the flatbuffers,
// nuspell, zola, and alertmanager run's finding 1). It's found where its
// Portfile's variant names the port, and MacPorts, evaluating that
// variant, says so; a variant naming it only in data, or a dependent the
// index already names, isn't one again. impact doesn't suggest building
// it, since check --also builds default variants.
func TestADependentUnderAVariantIsFound(t *testing.T) {
	t.Parallel()
	e, _, base, _ := revisionFixture(t, nil)
	tree := editTree(t, e, base, map[string]string{
		"textproc/nuspell/Portfile":  "name nuspell\n",
		"textproc/enchant2/Portfile": "name enchant2\nvariant nuspell description {Use nuspell} {\n    depends_lib-append port:nuspell\n}\nvariant docs {\n    set note port:nuspell\n}\n",
		"textproc/hunspell/Portfile": "name hunspell\ndepends_lib port:nuspell\nvariant extra {\n    depends_run-append port:nuspell\n}\n",
	})
	e.PortReader = fakePorts{
		directories: map[string][]macports.PortInfo{
			"textproc/nuspell":  {{Name: "nuspell"}},
			"textproc/enchant2": {{Name: "enchant2"}},
			"textproc/hunspell": {{Name: "hunspell", Dependencies: []macports.Dependency{{Port: "nuspell", Phase: "lib"}}}},
		},
		withVariants: func(port macports.PortInfo, variants map[string]bool) macports.PortInfo {
			if port.Name == "enchant2" && variants["nuspell"] {
				port.Dependencies = append(port.Dependencies, macports.Dependency{Port: "nuspell", Phase: "lib", Spec: "port:nuspell"})
			}
			return port
		},
	}
	dependents, err := e.dependents(t.Context(), model.Source{Tree: tree}, []string{"textproc/nuspell"})
	require.NoError(t, err)
	require.Equal(t, []Dependent{
		{Name: "hunspell", Directory: "textproc/hunspell", On: []string{"nuspell"}, Phases: []string{"library"}},
		{Name: "enchant2", Directory: "textproc/enchant2", On: []string{"nuspell"}, Phases: []string{"library"}, Variants: []string{"nuspell"}},
	}, dependents)
	require.Equal(t, "enchant2 (library, under +nuspell)", dependents[1].Words())
	require.Equal(t, []Dependent{dependents[0]}, Impact{Dependents: dependents}.OneOfEachKind())
}

// A port that links a changed one only under a variant is a linked port
// too, listed apart and bumped with the rest, which --except can leave
// out: enchant2 links nuspell only under +nuspell, and --except enchant2
// was refused as not a library dependent (the flatbuffers, nuspell, zola,
// and alertmanager run's finding 1).
func TestALinkedPortUnderAVariantIsBumpedAndCanBeExcepted(t *testing.T) {
	t.Parallel()
	f := setup(t)
	harborMaster(t, f)
	write(t, f.upstream, map[string]string{"net/harbor-sync/Portfile": "PortGroup github 1.0\nname harbor-sync\nvariant sync description {Sync} {\n    depends_lib-append port:libharbor\n}\n"})
	testsupport.Git(t, f.upstream, "commit", "-q", "-am", "harbor-sync's variant")
	e := f.open(t)
	ports := harborPorts()
	ports.directories["net/harbor-sync"] = []macports.PortInfo{port("harbor-sync")}
	ports.withVariants = func(info macports.PortInfo, variants map[string]bool) macports.PortInfo {
		if info.Name == "harbor-sync" && variants["sync"] {
			info.Dependencies = append(info.Dependencies, macports.Dependency{Port: "libharbor", Phase: "lib", Spec: "port:libharbor"})
		}
		return info
	}
	e.PortReader = ports
	branch, err := e.Start(t.Context(), StartRequest{Name: "libharbor-3", Here: true})
	require.NoError(t, err)

	linked, err := e.LinkedPorts(t.Context(), branch, "libharbor", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"harbor-cli", "harbor-viewer", "harbor-sync"}, dependentNames(linked.Bump))
	require.Equal(t, []string{"sync"}, linked.Bump[2].Variants)
	linked, err = e.LinkedPorts(t.Context(), branch, "libharbor", []string{"harbor-sync"})
	require.NoError(t, err)
	require.Equal(t, []string{"harbor-sync"}, linked.Excepted)
	require.Equal(t, []string{"harbor-cli", "harbor-viewer"}, dependentNames(linked.Bump))
}

// tidy commits the library's update before the rebuilds for it, though a
// dependent's directory sorts first by name, textproc/aaa before
// textproc/jq (field testing, batch 12:
// games/taisei came before textproc/libunibreak).
func TestTheUpdateIsCommittedBeforeTheRebuildsForIt(t *testing.T) {
	t.Parallel()
	f := setup(t)
	write(t, f.upstream, map[string]string{"textproc/aaa/Portfile": "name aaa\nversion 1\n"})
	testsupport.Git(t, f.upstream, "add", "-A")
	testsupport.Git(t, f.upstream, "commit", "-q", "-m", "aaa")
	e, _ := f.withPreparer(t)
	branch, err := e.Start(t.Context(), StartRequest{Name: "jq-update"})
	require.NoError(t, err)
	_, err = e.Update(t.Context(), UpdateRequest{Branch: branch, Action: model.EditUpdate, Port: "jq"})
	require.NoError(t, err)
	e.PortReader = fakePorts{directories: map[string][]macports.PortInfo{"textproc/jq": {port("jq")}, "textproc/aaa": {port("aaa", "jq")}}}
	_, err = e.RevbumpLinked(t.Context(), branch, Update{Port: "jq", After: PortVersion{Version: "1.8.1"}}, nil, false)
	require.NoError(t, err)
	plan, err := e.PlanTidy(t.Context(), TidyRequest{Branch: branch})
	require.NoError(t, err)
	require.Len(t, plan.Groups, 2)
	require.Equal(t, "textproc/jq", plan.Groups[0].Directory, "the update first")
	require.Equal(t, "aaa: rebuild for jq 1.8.1", plan.Groups[1].Subject())
}
