package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The selection is embedded so that a release keeps its flat shape.
func TestReleaseSelectionIsFlat(t *testing.T) {
	release := Release{ReleaseSelection: ReleaseSelection{Requested: "2.0", CurrentVersion: "1.0", Stability: "stable"}, Version: "2.0", Tag: "v2.0"}
	raw, err := json.Marshal(release)
	require.NoError(t, err)
	var flat map[string]any
	require.NoError(t, json.Unmarshal(raw, &flat))
	require.Equal(t, "2.0", flat["Requested"])
	require.Equal(t, "1.0", flat["CurrentVersion"])
	require.NotContains(t, flat, "ReleaseSelection")
	var back Release
	require.NoError(t, json.Unmarshal(raw, &back))
	require.Equal(t, release, back)
}

// A release names the Git source a forge PortGroup's fetch clones: the
// repository's address with .git after it, at the release's tag, or at its
// commit where the Portfile pins one. Another repository, another ref, or
// a release found without a repository names none.
func TestAReleaseNamesTheGitSourceItsForgeClones(t *testing.T) {
	commit := "0123456789abcdef0123456789abcdef01234567"
	release := Release{Version: "4.0", Forge: "github", Instance: "https://github.com", Repository: "harbor/libharbor", Tag: "v4.0", Commit: commit}
	source := func(url, ref string) GitSource { return GitSource{URL: url, Ref: ref} }
	require.True(t, release.Names(source("https://github.com/harbor/libharbor.git", "v4.0")), "as github.setup writes it")
	require.True(t, release.Names(source("https://github.com/Harbor/LibHarbor", "v4.0")), "without .git, in another case")
	require.True(t, release.Names(source("https://github.com/harbor/libharbor.git", commit)), "a pinned commit")
	require.False(t, release.Names(source("https://github.com/harbor/libharbor.git", "v4.1")), "another tag")
	require.False(t, release.Names(source("https://github.com/harbor/libharbor.git", "")), "the default branch")
	require.False(t, release.Names(source("https://github.com/fork/libharbor.git", "v4.0")), "another repository")
	require.False(t, release.Names(source("https://gitlab.com/harbor/libharbor.git", "v4.0")), "another forge")
	archive := release
	archive.Archive = true
	require.False(t, archive.Names(source("https://github.com/harbor/libharbor.git", "v4.0")), "an archive's release")
	require.False(t, Release{Version: "4.0", Tag: "v4.0"}.Names(source("/v4.0", "v4.0")), "found without a repository")
	untagged := release
	untagged.Tag, untagged.Commit = "", ""
	require.False(t, untagged.Names(source("https://github.com/harbor/libharbor.git", "")), "no tag is no ref")
}
