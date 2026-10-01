package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/stretchr/testify/require"
)

func TestPushUsesExplicitExpectedHeadAndNeverPushesTags(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := snapshotRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	out, err := exec.CommandContext(t.Context(), "git", "init", "--bare", "-q", remote).CombinedOutput()
	require.NoError(t, err, "%s", out)
	a := snapshotCommit(t, repo, snapshotTree(t, repo, snapshotBlob(t, repo, "file", "one", 0100644)))
	b := snapshotCommit(t, repo, snapshotTree(t, repo, snapshotBlob(t, repo, "file", "two", 0100644)))
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/tags/unrelated", Desired: git.RefValue{Exists: true, Object: a}}}))
	require.NoError(t, repo.Push(t.Context(), git.Push{Remote: remote, Branch: "candidate", Commit: a}))
	// A stale 'absent' precondition may only no-op at the exact desired head.
	require.NoError(t, repo.Push(t.Context(), git.Push{Remote: remote, Branch: "candidate", Commit: a}))
	require.ErrorIs(t, repo.Push(t.Context(), git.Push{Remote: remote, Branch: "candidate", Commit: b}), git.ErrRefConflict)
	require.NoError(t, repo.Push(t.Context(), git.Push{Remote: remote, Branch: "candidate", Commit: b, ExpectedRemote: git.RefValue{Exists: true, Object: a}}))
	head, err := repo.RemoteHead(t.Context(), remote, "candidate")
	require.NoError(t, err)
	require.Equal(t, b, head.Object)
	out, err = exec.CommandContext(t.Context(), "git", "--git-dir", remote, "for-each-ref", "--format=%(refname)").CombinedOutput()
	require.NoError(t, err)
	require.Equal(t, "refs/heads/candidate\n", string(out))
}

// A branch's commits are counted by whether master has their change, by
// patch-id, as git cherry reads them: one picked onto master under
// another commit is master's, and one it lacks is the branch's own.
func TestCherryCountsWhatMasterHasOfABranch(t *testing.T) {
	repo, _ := portsCheckout(t)
	dir := repo.Root
	gitIn(t, dir, "switch", "-q", "-c", "dockhand/bump/jq-4f2a")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/Portfile"), []byte("name jq\nversion 2\n"), 0o644))
	gitIn(t, dir, "commit", "-q", "-am", "jq: update to 2")
	picked := gitIn(t, dir, "rev-parse", "HEAD")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "textproc/jq/files"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "textproc/jq/files/patch.diff"), []byte("+fix\n"), 0o644))
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "jq: fix the build")
	head := gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "switch", "-q", "master")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "elsewhere")
	gitIn(t, dir, "cherry-pick", picked)
	master := gitIn(t, dir, "rev-parse", "HEAD")

	equivalent, own, err := repo.Cherry(t.Context(), master, head)
	require.NoError(t, err)
	require.Equal(t, [2]int{1, 1}, [2]int{equivalent, own})
	equivalent, own, err = repo.Cherry(t.Context(), head, head)
	require.NoError(t, err)
	require.Equal(t, [2]int{0, 0}, [2]int{equivalent, own}, "nothing beyond itself")
	_, _, err = repo.Cherry(t.Context(), "master", head)
	require.Error(t, err, "literal commits")
}
