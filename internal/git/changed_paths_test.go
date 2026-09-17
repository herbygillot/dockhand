package git_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/stretchr/testify/require"
)

func TestChangedPathsIncludesBothSidesOfMovesAndRawNames(t *testing.T) {
	t.Parallel()
	repo := snapshotRepo(t)
	before := snapshotTree(t, repo,
		snapshotBlob(t, repo, "old\tname", "same bytes", 0o100644),
		snapshotBlob(t, repo, "mode", "script", 0o100644),
		snapshotBlob(t, repo, "unchanged", "keep", 0o100644),
	)
	after := snapshotTree(t, repo,
		snapshotBlob(t, repo, "new\nname", "same bytes", 0o100644),
		snapshotBlob(t, repo, "mode", "script", 0o100755),
		snapshotBlob(t, repo, "unchanged", "keep", 0o100644),
	)
	out, err := exec.CommandContext(t.Context(), "git", "-C", repo.Root, "config", "diff.renames", "true").CombinedOutput()
	require.NoError(t, err, "%s", out)
	paths, err := repo.ChangedPaths(t.Context(), before, after)
	require.NoError(t, err)
	require.Equal(t, []string{"mode", "new\nname", "old\tname"}, paths)
	sig := git.Signature{Name: "Fixture", Email: "fixture@example.invalid", When: time.Now()}
	commit, err := repo.WriteCommit(t.Context(), git.Commit{Tree: before, Message: "base", Author: sig, Committer: sig})
	require.NoError(t, err)
	fromCommit, err := repo.ChangedPaths(t.Context(), commit, after)
	require.NoError(t, err)
	require.Equal(t, paths, fromCommit)
	paths, err = repo.ChangedPaths(t.Context(), commit, before)
	require.NoError(t, err)
	require.Empty(t, paths)
	for _, invalid := range []string{"HEAD", "--help", "", strings.Repeat("f", 40)} {
		_, err := repo.ChangedPaths(t.Context(), invalid, after)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = repo.ChangedPaths(ctx, before, after)
	require.ErrorIs(t, err, context.Canceled)
}

func TestAddedOrModifiedPathsMatchesGitAMSelection(t *testing.T) {
	t.Parallel()
	repo := snapshotRepo(t)
	before := snapshotTree(t, repo,
		snapshotBlob(t, repo, "deleted", "removed bytes", 0o100644),
		snapshotBlob(t, repo, "old-name", "rename bytes", 0o100644),
		snapshotBlob(t, repo, "modified", "before", 0o100644),
		snapshotBlob(t, repo, "mode", "script", 0o100644),
		snapshotBlob(t, repo, "type", "plain file", 0o100644),
	)
	after := snapshotTree(t, repo,
		snapshotBlob(t, repo, "added\tname", "new bytes", 0o100644),
		snapshotBlob(t, repo, "new-name", "rename bytes", 0o100644),
		snapshotBlob(t, repo, "modified", "after", 0o100644),
		snapshotBlob(t, repo, "mode", "script", 0o100755),
		snapshotBlob(t, repo, "type", "modified", 0o120000),
	)
	out, err := exec.CommandContext(t.Context(), "git", "-C", repo.Root, "config", "diff.renames", "false").CombinedOutput()
	require.NoError(t, err, string(out))
	paths, err := repo.AddedOrModifiedPaths(t.Context(), before, after)
	require.NoError(t, err)
	require.Equal(t, []string{"added\tname", "mode", "modified"}, paths)
	all, err := repo.ChangedPaths(t.Context(), before, after)
	require.NoError(t, err)
	require.Contains(t, all, "deleted")
	require.Contains(t, all, "old-name")
	require.Contains(t, all, "new-name")
	require.Contains(t, all, "type")
	paths, err = repo.AddedOrModifiedPaths(t.Context(), before, before)
	require.NoError(t, err)
	require.Empty(t, paths)
	_, err = repo.AddedOrModifiedPaths(t.Context(), "HEAD", after)
	require.Error(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = repo.AddedOrModifiedPaths(ctx, before, after)
	require.ErrorIs(t, err, context.Canceled)
}
