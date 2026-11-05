package reuse

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A result stands for a build now only while everything it recorded
// reading is the same: the environment, the variants, and each directory's
// tree, its own, _resources, and every active port's (decision 28).
func TestARecordedBuildStandsWhileItsInputsDo(t *testing.T) {
	target := model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}, Directory: "sysutils/jq"}
	recorded := model.NewTargetInputs("origin a", "sysutils/jq", "11", "22", nil,
		[]model.ActivePort{{Name: "oniguruma6", Spec: "@6.9.10_0", Directory: "devel/oniguruma6", Tree: "33", Archive: "sha256:44"}})
	require.Equal(t, []string{"sysutils/jq", "_resources", "devel/oniguruma6"}, Paths(recorded))
	now := map[string]model.ObjectID{"sysutils/jq": "11", "_resources": "22", "devel/oniguruma6": "33"}
	require.True(t, Current(recorded, "origin a", target, nil, now, Judge{}))

	require.False(t, Current(recorded, "origin b", target, nil, now, Judge{}), "the environment was made again")
	require.False(t, Current(recorded, "", target, nil, now, Judge{}), "the environment can't say what it is")
	for path, why := range map[string]string{"sysutils/jq": "the port changed", "_resources": "a PortGroup changed", "devel/oniguruma6": "a dependency changed"} {
		changed := map[string]model.ObjectID{"sysutils/jq": "11", "_resources": "22", "devel/oniguruma6": "33"}
		changed[path] = "99"
		require.False(t, Current(recorded, "origin a", target, nil, changed, Judge{}), why)
	}
	gone := map[string]model.ObjectID{"sysutils/jq": "11", "_resources": "22"}
	require.False(t, Current(recorded, "origin a", target, nil, gone, Judge{}), "a dependency's directory is gone")
	variant := target
	variant.Target.Variants = map[string]bool{"doc": true}
	require.False(t, Current(recorded, "origin a", variant, nil, now, Judge{}), "other variants asked for")
	// Everything the result read is the same but whose directory it was:
	// a result recorded for another port's directory isn't this one's. No
	// test noticed this check removed (the test plan's escaped mutant).
	elsewhere := target
	elsewhere.Directory = "textproc/jq"
	require.False(t, Current(recorded, "origin a", elsewhere, nil, now, Judge{}), "a result recorded for another directory")

	incomplete := recorded
	incomplete.Active = []model.ActivePort{{Name: "oniguruma6", Spec: "@6.9.10_0", Directory: "devel/oniguruma6", Tree: "33"}}
	require.False(t, Current(incomplete, "origin a", target, nil, now, Judge{}), "an archive wasn't known, so the inputs are incomplete")

	outside := recorded
	outside.Active = []model.ActivePort{{Name: "legacy", Spec: "@1_0", Archive: "sha256:55"}}
	require.Equal(t, []string{"sysutils/jq", "_resources"}, Paths(outside), "a port with no directory has none to look up")
	require.False(t, Current(outside, "origin a", target, nil, now, Judge{}), "a port outside the tree leaves the inputs incomplete")
}

// A port fetched with Git reads, beside its tree, the commit its tag
// names, which the tag doesn't bind: its earlier build stands only where
// it recorded fetching the commit the plan expects now (batch 20). A tag
// moved since, a build recorded before dockhand kept the commit or whose
// provider couldn't say, and a ref the plan couldn't resolve establish
// nothing; a port fetched otherwise is as it was.
func TestAGitFetchedBuildStandsOnlyForTheCommitExpected(t *testing.T) {
	target := model.PlanTarget{ID: "harbor", Target: model.Target{Name: "harbor"}, Directory: "devel/harbor"}
	now := map[string]model.ObjectID{"devel/harbor": "11", "_resources": "22"}
	commit, moved := model.ObjectID(strings.Repeat("a", 40)), model.ObjectID(strings.Repeat("b", 40))
	recorded := model.NewTargetInputs("origin a", "devel/harbor", "11", "22", nil, []model.ActivePort{})
	recorded.Fetched = commit
	expected := &model.GitSource{URL: "https://github.com/harbor/harbor.git", Ref: "v4", Commit: commit, ResolvedAt: time.Now()}
	require.True(t, Current(recorded, "origin a", target, expected, now, Judge{}))

	tagMoved := *expected
	tagMoved.Commit = moved
	require.False(t, Current(recorded, "origin a", target, &tagMoved, now, Judge{}), "the same tree, and the tag names another commit now")
	before := recorded
	before.Fetched = ""
	require.False(t, Current(before, "origin a", target, expected, now, Judge{}), "recorded without the commit it fetched")
	unresolved := model.GitSource{URL: expected.URL, Ref: "v4", Unresolved: "its refs couldn't be read", ResolvedAt: time.Now()}
	require.False(t, Current(recorded, "origin a", target, &unresolved, now, Judge{}), "nothing to compare it with")
	short := model.GitSource{URL: expected.URL, Ref: "aaaaaaa", Abbreviation: "aaaaaaa", ResolvedAt: time.Now()}
	require.True(t, Current(recorded, "origin a", target, &short, now, Judge{}), "the commit an abbreviation begins")

	require.True(t, Current(before, "origin a", target, nil, now, Judge{}), "a port fetched otherwise, as before")
	require.False(t, Current(recorded, "origin a", target, nil, now, Judge{}), "fetched with Git then, and not now")
}

// A subport's result stands across an edit inside another subport's
// literal block, the directory's tree changed by that alone (blocks); an
// edit in its own block, outside every block, or anything blocks can't
// say reuses nothing, as before. An active port in a changed directory is
// read by the same rule (field testing's terraform replay, 2026-10-22).
func TestASubportsResultStandsAcrossAnotherSubportsEdit(t *testing.T) {
	sibling := model.PlanTarget{ID: "terraform-1.0", Target: model.Target{Name: "terraform-1.0", Subport: "terraform-1.0"}, Directory: "sysutils/terraform"}
	recorded := model.NewTargetInputs("origin a", "sysutils/terraform", "11", "22", nil,
		[]model.ActivePort{{Name: "terraform_select", Spec: "@1_0", Directory: "sysutils/terraform", Tree: "11", Archive: "sha256:44"}})
	now := map[string]model.ObjectID{"sysutils/terraform": "12", "_resources": "22"}
	inside := func(directory string, then, now model.ObjectID) ([]string, bool) {
		require.Equal(t, "sysutils/terraform", directory)
		require.Equal(t, model.ObjectID("11"), then)
		require.Equal(t, model.ObjectID("12"), now)
		return []string{"terraform-1.16"}, true
	}
	require.True(t, Current(recorded, "origin a", sibling, nil, now, Judge{Blocks: inside}), "terraform-1.16's block alone changed")
	require.False(t, Current(recorded, "origin a", sibling, nil, now, Judge{}), "trees alone, as before")
	own := func(string, model.ObjectID, model.ObjectID) ([]string, bool) { return []string{"terraform-1.0"}, true }
	require.False(t, Current(recorded, "origin a", sibling, nil, now, Judge{Blocks: own}), "its own block changed")
	unknown := func(string, model.ObjectID, model.ObjectID) ([]string, bool) { return nil, false }
	require.False(t, Current(recorded, "origin a", sibling, nil, now, Judge{Blocks: unknown}), "outside every block, or unreadable")
	selectChanged := func(string, model.ObjectID, model.ObjectID) ([]string, bool) {
		return []string{"terraform_select"}, true
	}
	require.False(t, Current(recorded, "origin a", sibling, nil, now, Judge{Blocks: selectChanged}), "an active port's own block changed")
}

// A result whose build named what it read of _resources stands across a
// change to a PortGroup it didn't load, and not across one to a PortGroup
// it did, or to a file every port reads, or an entry added beside them
// (the oracle survey's Q2): each of _resources' commits voided every kept
// result, where 175 of 191 in a year changed only the ports loading the
// PortGroup they changed. ResourcesChanged says what changed.
func TestAResultLastsUntilWhatItReadOfResourcesChanges(t *testing.T) {
	target := model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}, Directory: "sysutils/jq"}
	recorded, err := Inputs(t.Context(), resources("t1", cmakeBlob, golangBlob, "c1"), "t1", "origin", target, []model.ActivePort{},
		map[string]model.ObjectID{"_resources/port1.0/group/cmake-1.1.tcl": model.ObjectID(cmakeBlob)})
	require.NoError(t, err)
	now := func(revision trees, tree string) map[string]model.ObjectID {
		objects, err := Objects(t.Context(), revision, model.ObjectID(tree), Paths(recorded))
		require.NoError(t, err)
		return objects
	}
	require.True(t, Current(recorded, "origin", target, nil, now(resources("t1", cmakeBlob, golangBlob, "c1"), "t1"), Judge{}))

	golang := now(resources("t2", cmakeBlob, strings.Repeat("f", 40), "c1"), "t2")
	require.True(t, Current(recorded, "origin", target, nil, golang, Judge{}), "a PortGroup it didn't load changed")
	require.Empty(t, ResourcesChanged(recorded, golang))

	cmake := now(resources("t3", cmakeEdited, golangBlob, "c1"), "t3")
	require.False(t, Current(recorded, "origin", target, nil, cmake, Judge{}), "a PortGroup it loaded changed")
	require.Equal(t, "the cmake 1.1 PortGroup", ResourcesChanged(recorded, cmake))

	compilers := now(resources("t4", cmakeBlob, golangBlob, "c2"), "t4")
	require.False(t, Current(recorded, "origin", target, nil, compilers, Judge{}), "the compiler lists every port reads changed")
	require.Equal(t, "_resources/port1.0/compilers", ResourcesChanged(recorded, compilers))

	added := resources("t5", cmakeBlob, golangBlob, "c1")
	added["ls:t5p"] += " sdk tree s1"
	require.Equal(t, "_resources/port1.0/sdk", ResourcesChanged(recorded, now(added, "t5")), "an entry beside the PortGroups added")
	gone := resources("t6", cmakeBlob, golangBlob, "c1")
	gone["ls:t6g"] = "golang-1.0.tcl blob " + golangBlob
	require.Equal(t, "the cmake 1.1 PortGroup", ResourcesChanged(recorded, now(gone, "t6")), "a PortGroup it loaded gone")

	unread := map[string]model.ObjectID{"sysutils/jq": "11", "_resources": "t1r"}
	require.False(t, Current(recorded, "origin", target, nil, unread, Judge{}), "_resources' tree alone says nothing of its parts")
}

// Inputs recorded under _resources' whole tree, as every build was before
// a provider said what PortGroups it loaded, still stand, as a stricter
// key: for the same tree, and for no other, however little changed.
func TestAWholeTreeInputMatchesOnlyItsTree(t *testing.T) {
	target := model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}, Directory: "sysutils/jq"}
	recorded := model.NewTargetInputs("origin", "sysutils/jq", "11", "t1r", nil, []model.ActivePort{})
	same, err := Objects(t.Context(), resources("t1", cmakeBlob, golangBlob, "c1"), "t1", Paths(recorded))
	require.NoError(t, err)
	require.True(t, Current(recorded, "origin", target, nil, same, Judge{}))
	golang, err := Objects(t.Context(), resources("t2", cmakeBlob, strings.Repeat("f", 40), "c1"), "t2", Paths(recorded))
	require.NoError(t, err)
	require.False(t, Current(recorded, "origin", target, nil, golang, Judge{}), "a PortGroup changed, which it may have read")
	require.Equal(t, "_resources", ResourcesChanged(recorded, golang))
}

// A result lasts until a PortGroup loaded by an active port its
// environment built from source for the check changes, as well as its
// own: that build sourced them, and the result read what it made
// (prioritization's ruling, 2026-11-01). A port installed from an
// archive, MacPorts' or one dockhand kept, is named by its directory and
// archive alone, so a change to a PortGroup it loads leaves the result
// standing. ResourcesChanged says which port's build loaded what changed.
func TestAResultLastsUntilAPortGroupOfAPortBuiltWithItChanges(t *testing.T) {
	target := model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}, Directory: "sysutils/jq"}
	// gettext was built from source, and loaded the cmake PortGroup; zlib
	// was installed from its archive, and loads the golang PortGroup.
	active := []model.ActivePort{
		{Name: "gettext", Spec: "@0.22.5_0", Directory: "devel/gettext", Archive: "sha256:11", PortGroups: map[string]model.ObjectID{"_resources/port1.0/group/cmake-1.1.tcl": model.ObjectID(cmakeBlob)}},
		{Name: "zlib", Spec: "@1.3.1_0", Directory: "archivers/zlib", Archive: "sha256:22"},
	}
	revision := func(tree, cmake, golang string) trees {
		found := resources(tree, cmake, golang, "c1")
		found[tree+":devel/gettext"], found[tree+":archivers/zlib"] = "g1", "z1"
		return found
	}
	recorded, err := Inputs(t.Context(), revision("t1", cmakeBlob, golangBlob), "t1", "origin", target, active, map[string]model.ObjectID{})
	require.NoError(t, err)
	require.Equal(t, active[0].PortGroups, recorded.Active[0].PortGroups, "what the port built here loaded")
	require.Nil(t, recorded.Active[1].PortGroups, "the archive names what was installed from it")
	now := func(revision trees, tree string) map[string]model.ObjectID {
		objects, err := Objects(t.Context(), revision, model.ObjectID(tree), Paths(recorded))
		require.NoError(t, err)
		return objects
	}
	require.True(t, Current(recorded, "origin", target, nil, now(revision("t1", cmakeBlob, golangBlob), "t1"), Judge{}))

	installed := now(revision("t2", cmakeBlob, strings.Repeat("f", 40)), "t2")
	require.True(t, Current(recorded, "origin", target, nil, installed, Judge{}), "a PortGroup of a port installed from its archive changed")
	require.Empty(t, ResourcesChanged(recorded, installed))

	built := now(revision("t3", cmakeEdited, golangBlob), "t3")
	require.False(t, Current(recorded, "origin", target, nil, built, Judge{}), "a PortGroup of the port built with it changed")
	require.Equal(t, "the cmake 1.1 PortGroup, which gettext, built from source, loaded", ResourcesChanged(recorded, built))

	// A record from before, its active ports carrying none, is keyed as
	// it was: by its own PortGroups alone.
	old := recorded
	old.Active = []model.ActivePort{{Name: "gettext", Spec: "@0.22.5_0", Directory: "devel/gettext", Tree: "g1", Archive: "sha256:11"}, recorded.Active[1]}
	key, err := json.Marshal(old.Active[0])
	require.NoError(t, err)
	require.NotContains(t, string(key), "PortGroups", "left out, so the key is the one recorded")
	require.True(t, Current(old, "origin", target, nil, built, Judge{}), "an old record names no PortGroup of its ports")
}

// A build stands across a change that builds nothing differently: its
// target's directory, or an active port's, differing only in inert options
// (judge.Inert), each port judged said with what changed; and across a
// move, the directory it was built in gone and the port found by name,
// with a categories reorder or without one, the same tree carried with no
// options (the omnidiff session's note 8).
func TestABuildIsCarriedOverAChangeThatBuildsNothingDifferently(t *testing.T) {
	recorded := model.NewTargetInputs("origin a", "devel/omnidiff", "11", "22", nil,
		[]model.ActivePort{{Name: "omnidiff-data", Spec: "@1_0", Directory: "devel/omnidiff-data", Tree: "33", Archive: "sha256:44"}})
	target := model.PlanTarget{ID: "omnidiff", Target: model.Target{Name: "omnidiff"}, Directory: "devel/omnidiff"}
	var asked [][2]string
	maintainers := func(origin model.RunID, from string, then model.ObjectID, at string, now model.ObjectID) ([]string, bool) {
		require.Equal(t, model.RunID("run_766"), origin)
		asked = append(asked, [2]string{from, at})
		return []string{"maintainers"}, now != "99"
	}
	judge := Judge{Inert: maintainers}

	// The target's own Portfile: an openmaintainer line.
	edited := map[string]model.ObjectID{"devel/omnidiff": "12", "_resources": "22", "devel/omnidiff-data": "33"}
	carried, ok := Carry(recorded, "run_766", "origin a", target, nil, edited, judge)
	require.True(t, ok)
	require.Equal(t, []model.InertPort{{Port: "omnidiff", Options: []string{"maintainers"}}}, carried)
	require.False(t, Current(recorded, "origin a", target, nil, edited, Judge{}), "trees alone, as before")
	_, ok = Carry(recorded, "run_766", "origin a", target, nil, map[string]model.ObjectID{"devel/omnidiff": "99", "_resources": "22", "devel/omnidiff-data": "33"}, judge)
	require.False(t, ok, "a change that isn't inert")

	// An active port's: a dependent carries over by the same closure.
	dependency := map[string]model.ObjectID{"devel/omnidiff": "11", "_resources": "22", "devel/omnidiff-data": "34"}
	carried, ok = Carry(recorded, "run_766", "origin a", target, nil, dependency, judge)
	require.True(t, ok)
	require.Equal(t, []model.InertPort{{Port: "omnidiff-data", Options: []string{"maintainers"}}}, carried)

	// A move to textproc with its categories reordered: the directory it
	// was built in is gone, and the plan has it in textproc.
	moved := target
	moved.Directory = "textproc/omnidiff"
	asked = nil
	reordered := map[string]model.ObjectID{"textproc/omnidiff": "13", "_resources": "22", "devel/omnidiff-data": "33"}
	carried, ok = Carry(recorded, "run_766", "origin a", moved, nil, reordered, Judge{Inert: func(_ model.RunID, from string, _ model.ObjectID, at string, _ model.ObjectID) ([]string, bool) {
		asked = append(asked, [2]string{from, at})
		return []string{"categories"}, true
	}})
	require.True(t, ok)
	require.Equal(t, []model.InertPort{{Port: "omnidiff", Options: []string{"categories"}, From: "devel/omnidiff"}}, carried)
	require.Equal(t, [][2]string{{"devel/omnidiff", "textproc/omnidiff"}}, asked, "the new directory judged against the recorded tree")
	// And without a reorder: the same tree in another directory.
	carried, ok = Carry(recorded, "run_766", "origin a", moved, nil, map[string]model.ObjectID{"textproc/omnidiff": "11", "_resources": "22", "devel/omnidiff-data": "33"}, judge)
	require.True(t, ok)
	require.Equal(t, []model.InertPort{{Port: "omnidiff", From: "devel/omnidiff"}}, carried)
	_, ok = Carry(recorded, "run_766", "origin a", moved, nil, map[string]model.ObjectID{"textproc/omnidiff": "11", "_resources": "22", "devel/omnidiff-data": "33"}, Judge{})
	require.False(t, ok, "a move is followed only where inert changes are")
	_, ok = Carry(recorded, "run_766", "origin a", moved, nil, map[string]model.ObjectID{"devel/omnidiff": "11", "textproc/omnidiff": "11", "_resources": "22", "devel/omnidiff-data": "33"}, judge)
	require.False(t, ok, "the directory it was built in is still there: another port's")

	// An active port that moved is found by its name.
	movedData := map[string]model.ObjectID{"devel/omnidiff": "11", "_resources": "22", "textproc/omnidiff-data": "33"}
	lookup := func(port string) (string, bool) {
		return "textproc/omnidiff-data", port == "omnidiff-data"
	}
	carried, ok = Carry(recorded, "run_766", "origin a", target, nil, movedData, Judge{Inert: maintainers, Moved: lookup})
	require.True(t, ok)
	require.Equal(t, []model.InertPort{{Port: "omnidiff-data", From: "devel/omnidiff-data"}}, carried)
	_, ok = Carry(recorded, "run_766", "origin a", target, nil, movedData, Judge{Inert: maintainers})
	require.False(t, ok, "nothing finds it")
}

// A build is carried over a Portfile that only declares ports it had
// active, as tidy declares what a check found it linking, the ports
// declared said; one declaring a port it didn't have active builds again
// (#49; docs/designs/declare-linked-libraries.md, part 3).
func TestABuildIsCarriedOverADeclarationOfWhatItHadActive(t *testing.T) {
	recorded := model.NewTargetInputs("origin a", "devel/omnidiff", "11", "22", nil,
		[]model.ActivePort{{Name: "libiconv", Spec: "@1.18_0", Directory: "textproc/libiconv", Tree: "33", Archive: "sha256:44"}})
	target := model.PlanTarget{ID: "omnidiff", Target: model.Target{Name: "omnidiff"}, Directory: "devel/omnidiff"}
	declares := func(ports ...string) Declared {
		return func(origin model.RunID, directory string, then, now model.ObjectID) ([]string, bool) {
			require.Equal(t, "devel/omnidiff", directory)
			return ports, true
		}
	}
	trees := map[string]model.ObjectID{"devel/omnidiff": "12", "_resources": "22", "textproc/libiconv": "33"}
	carried, ok := Carry(recorded, "run_12", "origin a", target, nil, trees, Judge{Declared: declares("libiconv")})
	require.True(t, ok)
	require.Equal(t, []model.InertPort{{Port: "omnidiff", Declared: []string{"libiconv"}}}, carried)
	_, ok = Carry(recorded, "run_12", "origin a", target, nil, trees, Judge{Declared: declares("zlib")})
	require.False(t, ok, "zlib wasn't active as it built")
}
