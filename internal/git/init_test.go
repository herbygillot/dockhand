package git_test

import (
	"os"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/stretchr/testify/require"
)

// Init makes an empty repository whose unborn branch is the one named, and
// refuses a name that's no branch.
func TestInitMakesAnEmptyRepositoryOnItsBranch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	repo, err := git.Init(t.Context(), root, "master", git.Program{Env: os.Environ()})
	require.NoError(t, err)
	master, err := repo.ReadRef(t.Context(), "refs/heads/master")
	require.NoError(t, err)
	require.False(t, master.Exists, "no commit yet")
	branch, err := repo.CurrentBranch(t.Context())
	require.NoError(t, err)
	require.Equal(t, "master", branch)
	_, err = git.Init(t.Context(), t.TempDir(), "a..b", git.Program{Env: os.Environ()})
	require.Error(t, err)
}
