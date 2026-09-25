package engine

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateReadsGitHubURLs(t *testing.T) {
	for _, address := range []string{"https://github.com/rift-dev/rift", "github.com/rift-dev/rift", "https://github.com/rift-dev/rift.git", "https://github.com/rift-dev/rift/releases/tag/v0.4.2"} {
		name, err := githubName(address)
		require.NoError(t, err, address)
		require.Equal(t, "rift-dev/rift", name)
	}
	_, err := githubName("https://gitlab.com/rift-dev/rift")
	require.ErrorContains(t, err, "create reads projects on GitHub so far")
	_, err = githubName("https://github.com/rift-dev")
	require.ErrorContains(t, err, "names no repository")
}
