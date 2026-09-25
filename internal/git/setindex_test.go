package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
)

// SetIndex restores a recorded index entry by entry: a staged version
// comes back, a deletion is staged again, the working files stay as they
// are, and in a sparse checkout the paths outside it keep their
// skip-worktree bits instead of reading as deleted.
func TestSetIndexRestoresARecordedIndexInASparseCheckout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ports")
	for name, text := range map[string]string{"a/Portfile": "a\n", "a/gone": "gone\n", "b/Portfile": "b\n"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(text), 0o644))
	}
	gitIn(t, root, "init", "-q", "-b", "master")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "init")
	worktree := filepath.Join(filepath.Dir(root), "branch")
	gitIn(t, root, "worktree", "add", "-q", worktree, "-b", "branch")
	gitIn(t, worktree, "sparse-checkout", "set", "--no-cone", "/a/")
	require.NoFileExists(t, filepath.Join(worktree, "b/Portfile"))

	require.NoError(t, os.WriteFile(filepath.Join(worktree, "a/Portfile"), []byte("staged\n"), 0o644))
	gitIn(t, worktree, "add", "a/Portfile")
	gitIn(t, worktree, "rm", "-q", "--cached", "a/gone")
	repo, err := git.Open(t.Context(), worktree, "")
	require.NoError(t, err)
	recorded, err := repo.IndexTree(t.Context())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(worktree, "a/Portfile"), []byte("working\n"), 0o644))
	require.NoError(t, repo.ResetIndex(t.Context()))

	require.NoError(t, repo.SetIndex(t.Context(), recorded))
	require.Equal(t, "staged", gitIn(t, worktree, "show", ":a/Portfile"))
	require.Equal(t, "MM a/Portfile\nD  a/gone", gitIn(t, worktree, "status", "--short", "--untracked-files=no"), "b/Portfile keeps its skip-worktree bit")
	after, err := repo.IndexTree(t.Context())
	require.NoError(t, err)
	require.Equal(t, recorded, after)
	data, err := os.ReadFile(filepath.Join(worktree, "a/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "working\n", string(data))
	require.NoError(t, repo.SetIndex(t.Context(), recorded), "restoring what is already there changes nothing")
}
