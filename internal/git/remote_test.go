package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/v2/internal/git"
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
func TestContributionChecksOneCommitAndActualUpstreamAncestry(t *testing.T) {
	repo := snapshotRepo(t)
	base := snapshotCommit(t, repo, snapshotTree(t, repo, snapshotBlob(t, repo, "Portfile", "one", 0100644)))
	tree := snapshotTree(t, repo, snapshotBlob(t, repo, "Portfile", "two", 0100644))
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
	commit, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Parents: []string{base}, Message: "port: update\n\nDetails", Author: sig, Committer: sig})
	require.NoError(t, err)
	message, paths, err := repo.Contribution(t.Context(), base, commit)
	require.NoError(t, err)
	require.Equal(t, "port: update\n\nDetails", message)
	require.Equal(t, []string{"Portfile"}, paths)
	_, _, err = repo.Contribution(t.Context(), base, base)
	require.Error(t, err)
	remote := filepath.Join(t.TempDir(), "remote.git")
	out, err := exec.CommandContext(t.Context(), "git", "init", "--bare", "-q", remote).CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.NoError(t, repo.Push(t.Context(), git.Push{Remote: remote, Branch: "main", Commit: base}))
	require.NoError(t, repo.CheckContributionBase(t.Context(), remote, "main", base, commit))
	require.NoFileExists(t, filepath.Join(repo.CommonDir, "FETCH_HEAD"))
	require.NoError(t, repo.Push(t.Context(), git.Push{Remote: remote, Branch: "main", Commit: commit, ExpectedRemote: git.RefValue{Exists: true, Object: base}}))
	require.ErrorContains(t, repo.CheckContributionBase(t.Context(), remote, "main", base, commit), "already in")
	require.ErrorContains(t, repo.CheckContributionBase(t.Context(), remote, "missing", base, commit), "missing")
}
