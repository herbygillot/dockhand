package macports

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A port fetched with Git declares what it fetches by git.url and
// git.branch, as MacPorts evaluates them; a port fetched otherwise
// declares none, and what the evaluation couldn't settle is an error, not
// a port fetched otherwise.
func TestAGitFetchedPortDeclaresItsRepositoryAndRef(t *testing.T) {
	t.Parallel()
	port := PortInfo{Options: map[string]string{"fetch.type": "git", "git.url": "https://github.com/harbor/harbor.git", "git.branch": "v4.0"}}
	require.True(t, port.GitFetched())
	source, git, err := port.GitSource()
	require.NoError(t, err)
	require.True(t, git)
	require.Equal(t, model.GitSource{URL: "https://github.com/harbor/harbor.git", Ref: "v4.0"}, source)

	delete(port.Options, "git.branch")
	source, _, err = port.GitSource()
	require.NoError(t, err)
	require.Empty(t, source.Ref, "the repository's default branch")

	archive := PortInfo{Options: map[string]string{"fetch.type": "standard", "git.url": "https://github.com/harbor/harbor.git"}}
	require.False(t, archive.GitFetched())
	_, git, err = archive.GitSource()
	require.NoError(t, err)
	require.False(t, git)

	for name, broken := range map[string]PortInfo{
		"fetch.type unsettled": {Options: map[string]string{"git.url": "https://github.com/harbor/harbor.git"}, OptionErrors: map[string]string{"fetch.type": "can't read \"x\""}},
		"git.url unsettled":    {Options: map[string]string{"fetch.type": "git"}, OptionErrors: map[string]string{"git.url": "can't read \"x\""}},
		"no git.url":           {Options: map[string]string{"fetch.type": "git"}},
		"git.branch unsettled": {Options: map[string]string{"fetch.type": "git", "git.url": "https://github.com/harbor/harbor.git"}, OptionErrors: map[string]string{"git.branch": "can't read \"x\""}},
	} {
		_, _, err := broken.GitSource()
		require.Error(t, err, name)
	}
	require.False(t, PortInfo{OptionErrors: map[string]string{"fetch.type": "can't read \"x\""}}.GitFetched(), "unsettled isn't fetched with Git")
}

// A port the github PortGroup sets up to fetch a tag's archive takes its
// source from that tag, git.branch, in its repository, git.url: the tag
// a check records the commit of. One fetched with Git, not set up from
// GitHub, or fetching an uploaded release asset takes none.
func TestAGitHubArchivePortTakesItsSourceFromATag(t *testing.T) {
	t.Parallel()
	options := func(more ...string) PortInfo {
		port := PortInfo{Options: map[string]string{"fetch.type": "standard", "github.author": "harbor", "github.project": "harbor",
			"git.url": "https://github.com/harbor/harbor.git", "git.branch": "v4.0"}}
		for i := 0; i < len(more); i += 2 {
			port.Options[more[i]] = more[i+1]
		}
		return port
	}
	for _, from := range []string{"", "tarball", "tags", "archive"} {
		tag, ok := options("github.tarball_from", from).GitHubTag()
		require.True(t, ok, from)
		require.Equal(t, model.GitSource{URL: "https://github.com/harbor/harbor.git", Ref: "v4.0"}, tag)
	}
	for name, port := range map[string]PortInfo{
		"fetched with Git":     options("fetch.type", "git"),
		"not from GitHub":      options("github.author", ""),
		"a release asset":      options("github.tarball_from", "releases"),
		"no tag":               options("git.branch", ""),
		"fetch.type unsettled": {Options: options().Options, OptionErrors: map[string]string{"fetch.type": "can't read \"x\""}},
		"git.branch unsettled": {Options: options().Options, OptionErrors: map[string]string{"git.branch": "can't read \"x\""}},
	} {
		_, ok := port.GitHubTag()
		require.False(t, ok, name)
	}
}
