package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/stretchr/testify/require"
)

func snapshotRepo(t *testing.T) *git.Repository {
	t.Helper()
	root := t.TempDir()
	output, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", root).CombinedOutput()
	require.NoError(t, err, "%s", output)
	repo, err := git.Open(t.Context(), root, "")
	require.NoError(t, err)
	return repo
}
func snapshotBlob(t *testing.T, repo *git.Repository, name, body string, mode uint32) git.TreeEntry {
	t.Helper()
	object, err := repo.WriteBlob(t.Context(), []byte(body))
	require.NoError(t, err)
	return git.TreeEntry{Name: name, Object: object, Type: "blob", Mode: mode}
}
func snapshotTree(t *testing.T, repo *git.Repository, entries ...git.TreeEntry) string {
	t.Helper()
	object, err := repo.WriteTree(t.Context(), entries)
	require.NoError(t, err)
	return object
}
func snapshotCommit(t *testing.T, repo *git.Repository, tree string) string {
	t.Helper()
	sig := git.Signature{Name: "Fixture", Email: "test@example.invalid", When: time.Now()}
	id, err := repo.WriteCommit(t.Context(), git.Commit{Tree: tree, Message: "fixture", Author: sig, Committer: sig})
	require.NoError(t, err)
	return id
}

func TestSnapshotUsesRawWholeTreeAndLeavesCheckoutAlone(t *testing.T) {
	repo := snapshotRepo(t)
	subtree := snapshotTree(t, repo, snapshotBlob(t, repo, "helper.tcl", "set version 2\n", 0100644))
	tree := snapshotTree(t, repo,
		snapshotBlob(t, repo, ".gitattributes", "* export-subst\nPortfile export-ignore\n", 0100644),
		snapshotBlob(t, repo, "Portfile", "version 1\n$Format:%H$\n", 0100644),
		snapshotBlob(t, repo, "executable", "#!/bin/sh\n", 0100755),
		snapshotBlob(t, repo, "link", "_resources/helper.tcl", 0120000),
		git.TreeEntry{Name: "_resources", Object: subtree, Mode: 040000, Type: "tree"})
	commit := snapshotCommit(t, repo, tree)
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/heads/candidate", Desired: git.RefValue{Exists: true, Object: commit}}}))
	require.NoError(t, os.WriteFile(filepath.Join(repo.Root, "Portfile"), []byte("dirty checkout"), 0600))
	boundCommit, boundTree, err := repo.Branch(t.Context(), "candidate")
	require.NoError(t, err)
	require.Equal(t, commit, boundCommit)
	require.Equal(t, tree, boundTree)
	files, err := repo.Materialize(t.Context(), boundTree)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, files.Close()) })
	content, err := os.ReadFile(filepath.Join(files.Root, "Portfile"))
	require.NoError(t, err)
	require.Equal(t, "version 1\n$Format:%H$\n", string(content))
	content, err = os.ReadFile(filepath.Join(files.Root, "link"))
	require.NoError(t, err)
	require.Equal(t, "set version 2\n", string(content))
	info, err := os.Stat(filepath.Join(files.Root, "executable"))
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&0100)
	content, err = os.ReadFile(filepath.Join(repo.Root, "Portfile"))
	require.NoError(t, err)
	require.Equal(t, "dirty checkout", string(content))
	require.NoError(t, files.Close())
	require.NoDirExists(t, files.Root)
}

func TestBranchSelectionIsLiteralAndReportsMissingBranches(t *testing.T) {
	repo := snapshotRepo(t)
	tree := snapshotTree(t, repo)
	commit := snapshotCommit(t, repo, tree)
	require.NoError(t, repo.UpdateRefs(t.Context(), []git.RefChange{{Name: "refs/tags/tag-only", Desired: git.RefValue{Exists: true, Object: commit}}}))
	for _, name := range []string{"gone", "tag-only"} {
		_, _, err := repo.Branch(t.Context(), name)
		require.ErrorIs(t, err, git.ErrBranchMissing)
	}
	for _, name := range []string{"", "refs/heads/main", "main~1", "main^{tree}", "--help", "../main"} {
		_, _, err := repo.Branch(t.Context(), name)
		require.Error(t, err)
	}
	_, err := repo.Materialize(t.Context(), commit)
	require.Error(t, err)
	_, err = repo.Materialize(t.Context(), strings.Repeat("a", 40))
	require.Error(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = repo.Materialize(ctx, tree)
	require.ErrorIs(t, err, context.Canceled)
}

func TestSnapshotRefusesExternalSymlinksAndSubmodules(t *testing.T) {
	repo := snapshotRepo(t)
	for _, target := range []string{"../outside", "/etc/passwd", "missing", "link"} {
		tree := snapshotTree(t, repo, snapshotBlob(t, repo, "link", target, 0120000))
		files, err := repo.Materialize(t.Context(), tree)
		require.Error(t, err)
		require.Nil(t, files)
	}
	empty := snapshotTree(t, repo)
	commit := snapshotCommit(t, repo, empty)
	tree := snapshotTree(t, repo, git.TreeEntry{Name: "module", Mode: 0160000, Type: "commit", Object: commit})
	files, err := repo.Materialize(t.Context(), tree)
	require.Error(t, err)
	require.Nil(t, files)
}
