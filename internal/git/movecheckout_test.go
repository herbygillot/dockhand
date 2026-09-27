package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
)

// MoveCheckout takes a sparse checkout's branch, index, and files back to
// an earlier commit, leaving the paths outside it outside, and refuses,
// changing nothing, where a local change would be overwritten.
func TestMoveCheckoutTakesASparseCheckoutBack(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ports")
	for name, text := range map[string]string{"a/Portfile": "a\n", "b/Portfile": "b\n", "c/Portfile": "c\n"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(text), 0o644))
	}
	gitIn(t, root, "init", "-q", "-b", "master")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "init")
	worktree := filepath.Join(filepath.Dir(root), "branch")
	gitIn(t, root, "worktree", "add", "-q", worktree, "-b", "branch")
	gitIn(t, worktree, "sparse-checkout", "set", "--no-cone", "/a/", "/c/")
	before := gitIn(t, worktree, "rev-parse", "HEAD")
	// Master moves a and b on, and a rebase brings them in.
	require.NoError(t, os.WriteFile(filepath.Join(root, "a/Portfile"), []byte("a2\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b/Portfile"), []byte("b2\n"), 0o644))
	gitIn(t, root, "commit", "-q", "-am", "a2 and b2")
	gitIn(t, worktree, "rebase", "-q", "master")
	after := gitIn(t, worktree, "rev-parse", "HEAD")
	require.NoFileExists(t, filepath.Join(worktree, "b/Portfile"))
	repo, err := git.Open(t.Context(), worktree, "")
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(worktree, "a/Portfile"), []byte("mine\n"), 0o644))
	require.Error(t, repo.MoveCheckout(t.Context(), after, before), "a local change to a file it moves")
	require.Equal(t, after, gitIn(t, worktree, "rev-parse", "HEAD"))
	gitIn(t, worktree, "checkout", "--", "a/Portfile")
	require.Error(t, repo.MoveCheckout(t.Context(), before, after), "the checkout isn't where it's moved from")

	require.NoError(t, os.WriteFile(filepath.Join(worktree, "c/Portfile"), []byte("kept\n"), 0o644))
	require.NoError(t, repo.MoveCheckout(t.Context(), after, before))
	require.Equal(t, before, gitIn(t, worktree, "rev-parse", "HEAD"))
	require.Equal(t, "branch", gitIn(t, worktree, "branch", "--show-current"))
	data, err := os.ReadFile(filepath.Join(worktree, "a/Portfile"))
	require.NoError(t, err)
	require.Equal(t, "a\n", string(data))
	require.Equal(t, "M c/Portfile", gitIn(t, worktree, "status", "--short", "--untracked-files=no"), "a change to a file it doesn't move stays, and b stays outside")
	require.NoFileExists(t, filepath.Join(worktree, "b/Portfile"))
}
