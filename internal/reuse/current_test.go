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
	require.True(t, Current(recorded, "origin a", target, nil, now))

	require.False(t, Current(recorded, "origin b", target, nil, now), "the environment was made again")
	require.False(t, Current(recorded, "", target, nil, now), "the environment can't say what it is")
	for path, why := range map[string]string{"sysutils/jq": "the port changed", "_resources": "a PortGroup changed", "devel/oniguruma6": "a dependency changed"} {
		changed := map[string]model.ObjectID{"sysutils/jq": "11", "_resources": "22", "devel/oniguruma6": "33"}
		changed[path] = "99"
		require.False(t, Current(recorded, "origin a", target, nil, changed), why)
	}
	gone := map[string]model.ObjectID{"sysutils/jq": "11", "_resources": "22"}
	require.False(t, Current(recorded, "origin a", target, nil, gone), "a dependency's directory is gone")
	variant := target
	variant.Target.Variants = map[string]bool{"doc": true}
	require.False(t, Current(recorded, "origin a", variant, nil, now), "other variants asked for")
	// Everything the result read is the same but whose directory it was:
	// a result recorded for another port's directory isn't this one's. No
	// test noticed this check removed (the test plan's escaped mutant).
	elsewhere := target
	elsewhere.Directory = "textproc/jq"
	require.False(t, Current(recorded, "origin a", elsewhere, nil, now), "a result recorded for another directory")

	incomplete := recorded
	incomplete.Active = []model.ActivePort{{Name: "oniguruma6", Spec: "@6.9.10_0", Directory: "devel/oniguruma6", Tree: "33"}}
	require.False(t, Current(incomplete, "origin a", target, nil, now), "an archive wasn't known, so the inputs are incomplete")

	outside := recorded
	outside.Active = []model.ActivePort{{Name: "legacy", Spec: "@1_0", Archive: "sha256:55"}}
	require.Equal(t, []string{"sysutils/jq", "_resources"}, Paths(outside), "a port with no directory has none to look up")
	require.False(t, Current(outside, "origin a", target, nil, now), "a port outside the tree leaves the inputs incomplete")
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
	require.True(t, Current(recorded, "origin a", target, expected, now))

	tagMoved := *expected
	tagMoved.Commit = moved
	require.False(t, Current(recorded, "origin a", target, &tagMoved, now), "the same tree, and the tag names another commit now")
	before := recorded
	before.Fetched = ""
	require.False(t, Current(before, "origin a", target, expected, now), "recorded without the commit it fetched")
	unresolved := model.GitSource{URL: expected.URL, Ref: "v4", Unresolved: "its refs couldn't be read", ResolvedAt: time.Now()}
	require.False(t, Current(recorded, "origin a", target, &unresolved, now), "nothing to compare it with")
	short := model.GitSource{URL: expected.URL, Ref: "aaaaaaa", Abbreviation: "aaaaaaa", ResolvedAt: time.Now()}
	require.True(t, Current(recorded, "origin a", target, &short, now), "the commit an abbreviation begins")

	require.True(t, Current(before, "origin a", target, nil, now), "a port fetched otherwise, as before")
	require.False(t, Current(recorded, "origin a", target, nil, now), "fetched with Git then, and not now")
}
