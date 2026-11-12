package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A Git source is built by a build that fetched the commit its ref named
// when it was resolved, or one its abbreviation begins; a build that
// fetched another built another source, and one that didn't say, or a
// source that couldn't be resolved, establishes nothing either way.
func TestAGitSourceIsBuiltByTheCommitItsRefNamed(t *testing.T) {
	commit, other := ObjectID(strings.Repeat("a", 40)), ObjectID(strings.Repeat("b", 40))
	tag := GitSource{URL: "https://example.org/harbor.git", Ref: "v4", Commit: commit, ResolvedAt: time.Now()}
	require.Equal(t, string(commit), tag.Expected())
	require.Equal(t, SourceSame, tag.AgainstFetched(commit))
	require.Equal(t, SourceMoved, tag.AgainstFetched(other), "the tag named another commit when it was fetched")
	require.Equal(t, SourceUnknown, tag.AgainstFetched(""), "a build that didn't say isn't said to have moved")
	require.Equal(t, SourceMoved, tag.AgainstFetched(commit[:12]), "a commit is fetched whole")

	short := GitSource{URL: tag.URL, Ref: "aaaaaaa", Abbreviation: "aaaaaaa", ResolvedAt: time.Now()}
	require.Equal(t, "aaaaaaa", short.Expected())
	require.Equal(t, SourceSame, short.AgainstFetched(commit), "the commit it abbreviates")
	require.Equal(t, SourceMoved, short.AgainstFetched(other))

	unresolved := GitSource{URL: tag.URL, Ref: "v4", Unresolved: "its refs couldn't be read", ResolvedAt: time.Now()}
	require.Empty(t, unresolved.Expected())
	require.Equal(t, SourceUnknown, unresolved.AgainstFetched(commit), "nothing establishes what wasn't resolved")
}

// What a Git-fetched build fetched is one of its inputs, and a key made
// without it, as every key was before, is the key it was: a port fetched
// otherwise keeps its recorded key.
func TestWhatABuildFetchedIsAnInputOnlyWhereItFetchedWithGit(t *testing.T) {
	inputs := NewTargetInputs("origin", "devel/libharbor", "11", "22", nil, []ActivePort{})
	data, err := json.Marshal(inputs)
	require.NoError(t, err)
	require.Equal(t, `{"Environment":"origin","Directory":"devel/libharbor","Tree":"11","Resources":"22","Active":[]}`, string(data), "the record a key was made from before it")
	require.Equal(t, "sha256:"+Digest(data), inputs.Key())
	fetched := inputs
	fetched.Fetched = ObjectID(strings.Repeat("a", 40))
	require.NotEqual(t, inputs.Key(), fetched.Key())
	other := inputs
	other.Fetched = ObjectID(strings.Repeat("b", 40))
	require.NotEqual(t, fetched.Key(), other.Key(), "another commit fetched is another build")
}

// One judgment of two readings of a source, every row of the design's two
// tables in both argument orders where the order can matter, and a record
// from before plans kept the commit (docs/designs/source-comparison.md).
func TestTwoReadingsOfASourceAreJudgedOneWay(t *testing.T) {
	commit, other := ObjectID(strings.Repeat("a", 40)), ObjectID(strings.Repeat("b", 40))
	at := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	source := func(ref string, commit ObjectID, abbreviation string) *GitSource {
		s := &GitSource{URL: "https://example.org/harbor.git", Ref: ref, Commit: commit, Abbreviation: abbreviation, ResolvedAt: at}
		if commit == "" && abbreviation == "" {
			s.Unresolved = "the repository couldn't be read"
		}
		return s
	}
	elsewhere := source("v4", commit, "")
	elsewhere.URL = "https://example.org/other.git"
	for _, c := range []struct {
		name       string
		have, want *GitSource
		standing   SourceStanding
	}{
		{"neither fetches this way", nil, nil, SourceSame},
		{"only one does", source("v4", commit, ""), nil, SourceOther},
		{"another repository", source("v4", commit, ""), elsewhere, SourceOther},
		{"an older release's tag is other, never moved", source("v1.7.1", commit, ""), source("v1.9.2", other, ""), SourceOther},
		{"two whole commits, equal", source("v4", commit, ""), source("v4", commit, ""), SourceSame},
		{"two whole commits, apart", source("v4", commit, ""), source("v4", other, ""), SourceMoved},
		{"two abbreviations", source("a1b2c3d", "", "a1b2c3d"), source("a1b2c3d", "", "a1b2c3d"), SourceSame},
		{"a whole commit and an abbreviation it begins with", source("v4", commit, ""), source("v4", "", "aaaaaaa"), SourceUnknown},
		{"one names a commit, the other nothing", source("v4", commit, ""), source("v4", "", ""), SourceUnknown},
		{"one names an abbreviation, the other nothing", source("v4", "", "aaaaaaa"), source("v4", "", ""), SourceUnknown},
		{"neither names anything", source("v4", "", ""), source("v4", "", ""), SourceUnresolved},
	} {
		require.Equal(t, c.standing, SourceAgainst(c.have, c.want), c.name)
		require.Equal(t, c.standing, SourceAgainst(c.want, c.have), "%s, the other way", c.name)
	}

	for _, c := range []struct {
		name     string
		plan     *GitSource
		fetched  ObjectID
		standing SourceStanding
	}{
		{"a whole commit, built", source("v4", commit, ""), commit, SourceSame},
		{"a whole commit, another built", source("v4", commit, ""), other, SourceMoved},
		{"an abbreviation the build begins with", source("a1b2", "", "aaaa"), commit, SourceSame},
		{"an abbreviation the build doesn't", source("a1b2", "", "aaaa"), other, SourceMoved},
		{"a commit, the build said nothing", source("v4", commit, ""), "", SourceUnknown},
		{"nothing named, the build said its commit", source("v4", "", ""), commit, SourceUnknown},
		{"nothing either side", source("v4", "", ""), "", SourceUnresolved},
	} {
		require.Equal(t, c.standing, c.plan.AgainstFetched(c.fetched), c.name)
	}

	// A record from before plans kept the commit names nothing, which is
	// a reading that couldn't be resolved, not one that moved.
	old := &GitSource{URL: "https://example.org/harbor.git", Ref: "v4"}
	require.Equal(t, SourceUnknown, SourceAgainst(old, source("v4", commit, "")))
	require.Equal(t, SourceUnknown, old.AgainstFetched(commit))
}
