package reuse

import (
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
	require.True(t, Current(recorded, "origin a", target, nil, now, nil))

	require.False(t, Current(recorded, "origin b", target, nil, now, nil), "the environment was made again")
	require.False(t, Current(recorded, "", target, nil, now, nil), "the environment can't say what it is")
	for path, why := range map[string]string{"sysutils/jq": "the port changed", "_resources": "a PortGroup changed", "devel/oniguruma6": "a dependency changed"} {
		changed := map[string]model.ObjectID{"sysutils/jq": "11", "_resources": "22", "devel/oniguruma6": "33"}
		changed[path] = "99"
		require.False(t, Current(recorded, "origin a", target, nil, changed, nil), why)
	}
	gone := map[string]model.ObjectID{"sysutils/jq": "11", "_resources": "22"}
	require.False(t, Current(recorded, "origin a", target, nil, gone, nil), "a dependency's directory is gone")
	variant := target
	variant.Target.Variants = map[string]bool{"doc": true}
	require.False(t, Current(recorded, "origin a", variant, nil, now, nil), "other variants asked for")
	// Everything the result read is the same but whose directory it was:
	// a result recorded for another port's directory isn't this one's. No
	// test noticed this check removed (the test plan's escaped mutant).
	elsewhere := target
	elsewhere.Directory = "textproc/jq"
	require.False(t, Current(recorded, "origin a", elsewhere, nil, now, nil), "a result recorded for another directory")

	incomplete := recorded
	incomplete.Active = []model.ActivePort{{Name: "oniguruma6", Spec: "@6.9.10_0", Directory: "devel/oniguruma6", Tree: "33"}}
	require.False(t, Current(incomplete, "origin a", target, nil, now, nil), "an archive wasn't known, so the inputs are incomplete")

	outside := recorded
	outside.Active = []model.ActivePort{{Name: "legacy", Spec: "@1_0", Archive: "sha256:55"}}
	require.Equal(t, []string{"sysutils/jq", "_resources"}, Paths(outside), "a port with no directory has none to look up")
	require.False(t, Current(outside, "origin a", target, nil, now, nil), "a port outside the tree leaves the inputs incomplete")
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
	require.True(t, Current(recorded, "origin a", target, expected, now, nil))

	tagMoved := *expected
	tagMoved.Commit = moved
	require.False(t, Current(recorded, "origin a", target, &tagMoved, now, nil), "the same tree, and the tag names another commit now")
	before := recorded
	before.Fetched = ""
	require.False(t, Current(before, "origin a", target, expected, now, nil), "recorded without the commit it fetched")
	unresolved := model.GitSource{URL: expected.URL, Ref: "v4", Unresolved: "its refs couldn't be read", ResolvedAt: time.Now()}
	require.False(t, Current(recorded, "origin a", target, &unresolved, now, nil), "nothing to compare it with")
	short := model.GitSource{URL: expected.URL, Ref: "aaaaaaa", Abbreviation: "aaaaaaa", ResolvedAt: time.Now()}
	require.True(t, Current(recorded, "origin a", target, &short, now, nil), "the commit an abbreviation begins")

	require.True(t, Current(before, "origin a", target, nil, now, nil), "a port fetched otherwise, as before")
	require.False(t, Current(recorded, "origin a", target, nil, now, nil), "fetched with Git then, and not now")
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
	require.True(t, Current(recorded, "origin a", sibling, nil, now, inside), "terraform-1.16's block alone changed")
	require.False(t, Current(recorded, "origin a", sibling, nil, now, nil), "trees alone, as before")
	own := func(string, model.ObjectID, model.ObjectID) ([]string, bool) { return []string{"terraform-1.0"}, true }
	require.False(t, Current(recorded, "origin a", sibling, nil, now, own), "its own block changed")
	unknown := func(string, model.ObjectID, model.ObjectID) ([]string, bool) { return nil, false }
	require.False(t, Current(recorded, "origin a", sibling, nil, now, unknown), "outside every block, or unreadable")
	selectChanged := func(string, model.ObjectID, model.ObjectID) ([]string, bool) {
		return []string{"terraform_select"}, true
	}
	require.False(t, Current(recorded, "origin a", sibling, nil, now, selectChanged), "an active port's own block changed")
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
	require.True(t, Current(recorded, "origin", target, nil, now(resources("t1", cmakeBlob, golangBlob, "c1"), "t1"), nil))

	golang := now(resources("t2", cmakeBlob, strings.Repeat("f", 40), "c1"), "t2")
	require.True(t, Current(recorded, "origin", target, nil, golang, nil), "a PortGroup it didn't load changed")
	require.Empty(t, ResourcesChanged(recorded, golang))

	cmake := now(resources("t3", cmakeEdited, golangBlob, "c1"), "t3")
	require.False(t, Current(recorded, "origin", target, nil, cmake, nil), "a PortGroup it loaded changed")
	require.Equal(t, "the cmake 1.1 PortGroup", ResourcesChanged(recorded, cmake))

	compilers := now(resources("t4", cmakeBlob, golangBlob, "c2"), "t4")
	require.False(t, Current(recorded, "origin", target, nil, compilers, nil), "the compiler lists every port reads changed")
	require.Equal(t, "_resources/port1.0/compilers", ResourcesChanged(recorded, compilers))

	added := resources("t5", cmakeBlob, golangBlob, "c1")
	added["ls:t5p"] += " sdk tree s1"
	require.Equal(t, "_resources/port1.0/sdk", ResourcesChanged(recorded, now(added, "t5")), "an entry beside the PortGroups added")
	gone := resources("t6", cmakeBlob, golangBlob, "c1")
	gone["ls:t6g"] = "golang-1.0.tcl blob " + golangBlob
	require.Equal(t, "the cmake 1.1 PortGroup", ResourcesChanged(recorded, now(gone, "t6")), "a PortGroup it loaded gone")

	unread := map[string]model.ObjectID{"sysutils/jq": "11", "_resources": "t1r"}
	require.False(t, Current(recorded, "origin", target, nil, unread, nil), "_resources' tree alone says nothing of its parts")
}

// Inputs recorded under _resources' whole tree, as every build was before
// a provider said what PortGroups it loaded, still stand, as a stricter
// key: for the same tree, and for no other, however little changed.
func TestAWholeTreeInputMatchesOnlyItsTree(t *testing.T) {
	target := model.PlanTarget{ID: "jq", Target: model.Target{Name: "jq"}, Directory: "sysutils/jq"}
	recorded := model.NewTargetInputs("origin", "sysutils/jq", "11", "t1r", nil, []model.ActivePort{})
	same, err := Objects(t.Context(), resources("t1", cmakeBlob, golangBlob, "c1"), "t1", Paths(recorded))
	require.NoError(t, err)
	require.True(t, Current(recorded, "origin", target, nil, same, nil))
	golang, err := Objects(t.Context(), resources("t2", cmakeBlob, strings.Repeat("f", 40), "c1"), "t2", Paths(recorded))
	require.NoError(t, err)
	require.False(t, Current(recorded, "origin", target, nil, golang, nil), "a PortGroup changed, which it may have read")
	require.Equal(t, "_resources", ResourcesChanged(recorded, golang))
}
