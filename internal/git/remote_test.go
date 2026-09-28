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
