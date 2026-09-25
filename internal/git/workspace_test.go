package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/git"
)

// portsCheckout makes a small ports tree with one commit and opens it.
func portsCheckout(t *testing.T) (*git.Repository, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "macports-ports")
	files := map[string]string{
		"_resources/port1.0/group/github-1.0.tcl": "# group\n",
		"textproc/jq/Portfile":                    "name jq\n",
		"devel/libharbor/Portfile":                "name libharbor\n",
		"README.md":                               "ports\n",
	}
	for name, content := range files {
		require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
	}
	gitIn(t, root, "init", "-q", "-b", "master")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "init")
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	head, err := repo.Resolve(t.Context(), "HEAD")
	require.NoError(t, err)
	return repo, head
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.org"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func files(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	require.NoError(t, filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" {
			// In a linked worktree .git is a file, and SkipDir on a file
			// would skip the rest of its directory.
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			found = append(found, rel)
		}
		return nil
	}))
	sort.Strings(found)
	return found
}

func TestCreateBranchRefusesToMoveOne(t *testing.T) {
	repo, head := portsCheckout(t)
	require.NoError(t, repo.CreateBranch(t.Context(), "dockhand/jq-update", head))
	require.ErrorIs(t, repo.CreateBranch(t.Context(), "dockhand/jq-update", head), git.ErrBranchExists)
	require.Error(t, repo.CreateBranch(t.Context(), "-bad", head))

	gitIn(t, repo.Root, "commit", "-q", "--allow-empty", "-m", "moved")
	moved, err := repo.Resolve(t.Context(), "HEAD")
	require.NoError(t, err)
	gitIn(t, repo.Root, "branch", "-f", "dockhand/jq-update", moved)
	require.Error(t, repo.DeleteBranch(t.Context(), "dockhand/jq-update", head), "a branch someone moved is not deleted")
	require.NoError(t, repo.DeleteBranch(t.Context(), "dockhand/jq-update", moved))
}

func TestSparseWorktreeHoldsOnlyItsConeAndLeavesTheCheckoutWhole(t *testing.T) {
	repo, head := portsCheckout(t)
	require.NoError(t, repo.CreateBranch(t.Context(), "dockhand/jq-update", head))
	dir := filepath.Join(t.TempDir(), "branches", "jq-update")
	require.NoError(t, repo.AddSparseWorktree(t.Context(), dir, "dockhand/jq-update", []string{"_resources"}))
	require.Equal(t, []string{"README.md", "_resources/port1.0/group/github-1.0.tcl"}, files(t, dir))

	worktree, err := git.Open(t.Context(), dir, "")
	require.NoError(t, err)
	require.Equal(t, repo.CommonDir, worktree.CommonDir, "one repository, two checkouts")
	branch, err := worktree.CurrentBranch(t.Context())
	require.NoError(t, err)
	require.Equal(t, "dockhand/jq-update", branch)
	changes, err := worktree.TrackedChanges(t.Context())
	require.NoError(t, err)
	require.Empty(t, changes, "files outside the cone are not deletions")

	require.NoError(t, worktree.ExpandSparse(t.Context(), "textproc/jq"))
	require.Contains(t, files(t, dir), "textproc/jq/Portfile")
	cone, err := worktree.SparseCone(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"_resources", "textproc/jq"}, cone)

	require.Len(t, files(t, repo.Root), 4, "the person's own checkout keeps every file")
	whole, err := repo.SparseCone(t.Context())
	require.NoError(t, err)
	require.Empty(t, whole)

	require.Error(t, repo.AddSparseWorktree(t.Context(), dir, "dockhand/jq-update", []string{"_resources"}), "an existing directory is refused")
	require.NoError(t, repo.RemoveWorktree(t.Context(), dir))
	_, err = os.Stat(dir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestAFailedWorktreeIsRemoved(t *testing.T) {
	repo, _ := portsCheckout(t)
	dir := filepath.Join(t.TempDir(), "missing-branch")
	require.Error(t, repo.AddSparseWorktree(t.Context(), dir, "dockhand/nowhere", []string{"_resources"}))
	_, err := os.Stat(dir)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NotContains(t, gitIn(t, repo.Root, "worktree", "list"), "missing-branch")
}

func TestTrackedChangesAndSwitch(t *testing.T) {
	repo, head := portsCheckout(t)
	require.NoError(t, repo.CreateBranch(t.Context(), "dockhand/here", head))
	require.NoError(t, os.WriteFile(filepath.Join(repo.Root, "untracked.txt"), []byte("x"), 0o644))
	changes, err := repo.TrackedChanges(t.Context())
	require.NoError(t, err)
	require.Empty(t, changes, "untracked files are not work a switch displaces")

	require.NoError(t, os.WriteFile(filepath.Join(repo.Root, "textproc/jq/Portfile"), []byte("name jq\nversion 2\n"), 0o644))
	gitIn(t, repo.Root, "mv", "README.md", "README")
	changes, err = repo.TrackedChanges(t.Context())
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"textproc/jq/Portfile", "README"}, changes)

	gitIn(t, repo.Root, "reset", "-q", "--hard")
	require.NoError(t, repo.Switch(t.Context(), "dockhand/here"))
	branch, err := repo.CurrentBranch(t.Context())
	require.NoError(t, err)
	require.Equal(t, "dockhand/here", branch)
	require.Error(t, repo.Switch(t.Context(), "dockhand/absent"))
}
