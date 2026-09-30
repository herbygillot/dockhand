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
	require.True(t, tag.BuiltBy(commit))
	require.False(t, tag.Moved(commit))
	require.False(t, tag.BuiltBy(other))
	require.True(t, tag.Moved(other), "the tag named another commit when it was fetched")
	require.False(t, tag.BuiltBy(""), "a build that didn't say")
	require.False(t, tag.Moved(""), "isn't said to have moved")
	require.False(t, tag.BuiltBy(commit[:12]), "a commit is fetched whole")

	short := GitSource{URL: tag.URL, Ref: "aaaaaaa", Abbreviation: "aaaaaaa", ResolvedAt: time.Now()}
	require.Equal(t, "aaaaaaa", short.Expected())
	require.True(t, short.BuiltBy(commit), "the commit it abbreviates")
	require.True(t, short.Moved(other))

	unresolved := GitSource{URL: tag.URL, Ref: "v4", Unresolved: "its refs couldn't be read", ResolvedAt: time.Now()}
	require.Empty(t, unresolved.Expected())
	require.False(t, unresolved.BuiltBy(commit), "nothing establishes what wasn't resolved")
	require.False(t, unresolved.Moved(commit))
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
