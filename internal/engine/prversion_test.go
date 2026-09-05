package engine

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/stretchr/testify/assert"
)

// The version another pull request takes its port to is read off its
// head branch only where dockhand minted that branch, by inverting the
// mint's own construction; everything else establishes nothing, and
// the note that follows declines to guess. The facts leave here on the
// PRFact with their source named, which is what lets verdict weigh
// them without knowing what a branch name looks like.
func TestHeadVersionReadsOnlyWhatDockhandMinted(t *testing.T) {
	cases := []struct {
		name, head, port string
		version, source  string
	}{
		{name: "a bump's branch carries its version",
			head: "dockhand/jq-1.9", port: "jq",
			version: "1.9", source: "its branch name dockhand/jq-1.9"},
		{name: "a port with hyphens is cut off whole, not at the first one",
			head: "dockhand/py-foo-2.0.1", port: "py-foo",
			version: "2.0.1", source: "its branch name dockhand/py-foo-2.0.1"},
		{name: "a hand-made branch outside the namespace is not read",
			head: "jq-1.9", port: "jq"},
		{name: "a hand-made branch that names another port is not this port's",
			head: "dockhand/jqdata-1.9", port: "jq"},
		{name: "a namespaced branch that carries no port is nothing",
			head: "dockhand/", port: "jq"},
		{name: "a revision bump's branch is a revision, not a version",
			head: "dockhand/jq-rev2", port: "jq"},
		{name: "a refresh's branch is not a version",
			head: "dockhand/jq-checksums", port: "jq"},
		{name: "housekeeping's branch is not a version",
			head: "dockhand/jq-housekeeping", port: "jq"},
		{name: "no port to search by reads nothing",
			head: "dockhand/jq-1.9", port: ""},
		{name: "no head at all reads nothing",
			head: "", port: "jq"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			version, source := headVersion(tc.head, tc.port)
			assert.Equal(t, tc.version, version)
			assert.Equal(t, tc.source, source, "a version and its source arrive together or not at all")
		})
	}
}

// The three non-bump constructions are declined by name; a slug whose
// tail merely begins with "rev" is a version, because only the
// revision bump's exact form — rev and digits — is dockhand's own.
func TestVersionTargetDeclinesTheOtherIntents(t *testing.T) {
	assert.True(t, versionTarget("1.9"))
	assert.True(t, versionTarget("2024.01.15"))
	assert.True(t, versionTarget("revolution"), "a word that starts with rev is not a revision")
	assert.True(t, versionTarget("rev"), "rev with no number is not the revision bump's construction")
	assert.False(t, versionTarget(""))
	assert.False(t, versionTarget("rev2"))
	assert.False(t, versionTarget("rev12"))
	assert.False(t, versionTarget("checksums"))
	assert.False(t, versionTarget("housekeeping"))
}

// This promotion's own version comes from the record's headline — the
// intent and the target the planner wrote — and only a bump has one.
func TestBumpVersionIsOnlyABumps(t *testing.T) {
	assert.Equal(t, "1.9", bumpVersion("bump", "1.9"))
	assert.Empty(t, bumpVersion("bump-revision", "rev2"), "a revision is not a version to compare against")
	assert.Empty(t, bumpVersion("refresh-checksums", "checksums"))
	assert.Empty(t, bumpVersion("", ""), "a branch with no record has nothing to weigh")
}

// The boundary mapping, end to end: what gh returned becomes the fact
// verdict weighs, with the head read against the port the list was
// searched by.
func TestPRFactsCarryTheHeadVersion(t *testing.T) {
	facts := prFacts([]gh.PullRequest{
		{Number: 3, Title: "jq: bump to 1.9", State: "open", HTMLURL: "https://x/3",
			Head: gh.PRHead{Ref: "dockhand/jq-1.9"}},
		{Number: 4, Title: "jq: fix the manpage", State: "open", HTMLURL: "https://x/4",
			Head: gh.PRHead{Ref: "fix-jq-manpage"}},
	}, "jq")
	assert.Len(t, facts, 2)
	assert.True(t, facts[0].Found && facts[1].Found, "every listed PR exists")
	assert.Equal(t, "1.9", facts[0].Version)
	assert.Equal(t, "its branch name dockhand/jq-1.9", facts[0].VersionSource)
	assert.Empty(t, facts[1].Version)
	assert.Empty(t, facts[1].VersionSource)
}
