package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/stretchr/testify/require"
)

func TestFetchBranchFreezesRemoteAndPreservesLocalRefs(t *testing.T) {
	t.Parallel()
	upstream, local := snapshotRepo(t), snapshotRepo(t)
	tree := snapshotTree(t, upstream, snapshotBlob(t, upstream, "Portfile", "version 2", 0100644))
	latest := snapshotCommit(t, upstream, tree)
	require.NoError(t, upstream.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/master", Desired: git.RefValue{Exists: true, Object: latest}}}))
	stale := snapshotCommit(t, local, snapshotTree(t, local, snapshotBlob(t, local, "Portfile", "version 1", 0100644)))
	for _, name := range []string{"refs/heads/master", "refs/remotes/origin/master", "refs/remotes/upstream/master"} {
		require.NoError(t, local.UpdateRefs(t.Context(), []git.RefChange{{Name: name, Desired: git.RefValue{Exists: true, Object: stale}}}))
	}
	before, err := local.ReadRefs(t.Context(), "refs/")
	require.NoError(t, err)
	fetchHead := filepath.Join(local.CommonDir, "FETCH_HEAD")
	require.NoError(t, os.WriteFile(fetchHead, []byte("user fetch result"), 0600))
	var wg sync.WaitGroup
	type result struct {
		commit, tree string
		err          error
	}
	results := make(chan result, 6)
	for range 6 {
		wg.Go(func() {
			commit, tree, err := local.FetchBranch(t.Context(), upstream.Root, "master")
			results <- result{commit, tree, err}
		})
	}
	wg.Wait()
	close(results)
	for result := range results {
		require.NoError(t, result.err)
		require.Equal(t, latest, result.commit)
		require.Equal(t, tree, result.tree)
	}
	after, err := local.ReadRefs(t.Context(), "refs/")
	require.NoError(t, err)
	require.Equal(t, before, after)
	data, err := os.ReadFile(fetchHead)
	require.NoError(t, err)
	require.Equal(t, "user fetch result", string(data))
	// A later fetch must observe the new upstream, not any cached local ref.
	next := snapshotCommit(t, upstream, snapshotTree(t, upstream, snapshotBlob(t, upstream, "Portfile", "version 3", 0100644)))
	require.NoError(t, upstream.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/master", Expected: git.RefValue{Exists: true, Object: latest}, Desired: git.RefValue{Exists: true, Object: next}}}))
	fetched, _, err := local.FetchBranch(t.Context(), upstream.Root, "master")
	require.NoError(t, err)
	require.Equal(t, next, fetched)
	trees, err := local.CommitTrees(t.Context(), []string{latest})
	require.NoError(t, err)
	require.Equal(t, tree, trees[latest], "the already bound source remains usable")
	// Removing the remote branch must fail despite a usable stale local master.
	require.NoError(t, upstream.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/master", Expected: git.RefValue{Exists: true, Object: next}}}))
	_, _, err = local.FetchBranch(t.Context(), upstream.Root, "master")
	require.Error(t, err)
	after, err = local.ReadRefs(t.Context(), "refs/")
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestFetchBranchUsesURLWithoutRemoteTrackingSideEffects(t *testing.T) {
	t.Parallel()
	remote, local := snapshotRepo(t), snapshotRepo(t)
	commit := snapshotCommit(t, remote, snapshotTree(t, remote, snapshotBlob(t, remote, "file", "contents", 0100644)))
	require.NoError(t, remote.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/master", Desired: git.RefValue{Exists: true, Object: commit}}}))
	out, err := exec.CommandContext(t.Context(), "git", "-C", local.Root, "remote", "add", "origin", remote.Root).CombinedOutput()
	require.NoError(t, err, "%s", out)
	fetched, _, err := local.FetchBranch(t.Context(), remote.Root, "master")
	require.NoError(t, err)
	require.Equal(t, commit, fetched)
	refs, err := local.ReadRefs(t.Context(), "refs/")
	require.NoError(t, err)
	require.Empty(t, refs)
	require.NoFileExists(t, filepath.Join(local.CommonDir, "FETCH_HEAD"))
}
