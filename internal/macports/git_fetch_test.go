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
