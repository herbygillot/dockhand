package git_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/stretchr/testify/require"
)

func TestEditTreePreservesUneditedObjectsAndChecksFilePreconditions(t *testing.T) {
	t.Parallel()
	repo := snapshotRepo(t)
	original := snapshotBlob(t, repo, "Portfile", "version 1\nrevision 0\n", 0o100755)
	sibling := snapshotBlob(t, repo, "keep", "untouched", 0o100644)
	port := snapshotTree(t, repo, original, sibling)
	category := snapshotTree(t, repo, git.TreeEntry{Name: "port", Object: port, Mode: 0o40000, Type: "tree"})
	resources := snapshotTree(t, repo, snapshotBlob(t, repo, "helper.tcl", "set helper 1", 0o100644))
	base := snapshotTree(t, repo, git.TreeEntry{Name: "devel", Object: category, Mode: 0o40000, Type: "tree"}, git.TreeEntry{Name: "_resources", Object: resources, Mode: 0o40000, Type: "tree"})
	before, data, err := repo.File(t.Context(), base, "devel/port/Portfile")
	require.NoError(t, err)
	require.Equal(t, "version 1\nrevision 0\n", string(data))
	require.Equal(t, original.Object, before.Blob)
	require.NoError(t, os.WriteFile(filepath.Join(repo.Root, "dirty"), []byte("user edit"), 0600))
	edits := []git.FileEdit{
		{Path: "devel/port/Portfile", Before: before, After: []byte("version 1\nrevision 1\n"), Mode: before.Mode},
		{Path: "devel/port/files/new.patch", After: []byte("patch"), Mode: 0o100644},
	}
	candidate, err := repo.EditTree(t.Context(), base, edits)
	require.NoError(t, err)
	actual, data, err := repo.File(t.Context(), candidate, "devel/port/Portfile")
	require.NoError(t, err)
	require.Equal(t, before.Mode, actual.Mode)
	require.Equal(t, "version 1\nrevision 1\n", string(data))
	entries, err := repo.ReadTree(t.Context(), candidate)
	require.NoError(t, err)
	require.Contains(t, entries, git.TreeEntry{Name: "_resources", Object: resources, Mode: 0o40000, Type: "tree"})
	unchanged, _, err := repo.File(t.Context(), candidate, "devel/port/keep")
	require.NoError(t, err)
	require.Equal(t, sibling.Object, unchanged.Blob)
	_, data, err = repo.File(t.Context(), base, "devel/port/Portfile")
	require.NoError(t, err)
	require.Equal(t, "version 1\nrevision 0\n", string(data))
	require.NoFileExists(t, filepath.Join(repo.CommonDir, "index"))
	dirty, err := os.ReadFile(filepath.Join(repo.Root, "dirty"))
	require.NoError(t, err)
	require.Equal(t, "user edit", string(dirty))
	result, err := repo.EditTree(t.Context(), candidate, edits)
	require.ErrorIs(t, err, git.ErrFilePrecondition)
	require.Empty(t, result)
	patch, err := repo.DiffTrees(t.Context(), base, candidate)
	require.NoError(t, err)
	require.Contains(t, string(patch), "-revision 0\n+revision 1")
	require.Contains(t, string(patch), "a/devel/port/Portfile")
	file, _, err := repo.File(t.Context(), candidate, "devel/port/files/new.patch")
	require.NoError(t, err)
	deleted, err := repo.EditTree(t.Context(), candidate, []git.FileEdit{{Path: "devel/port/files/new.patch", Before: file, Delete: true}})
	require.NoError(t, err)
	absent, _, err := repo.File(t.Context(), deleted, "devel/port/files/new.patch")
	require.NoError(t, err)
	require.False(t, absent.Exists)
}

func TestEditTreeRefusesInvalidOverlappingAndSymlinkPaths(t *testing.T) {
	t.Parallel()
	repo := snapshotRepo(t)
	base := snapshotTree(t, repo, snapshotBlob(t, repo, "link", "target", 0o120000), snapshotBlob(t, repo, "target", "data", 0o100644))
	for _, path := range []string{"", ".", "../escape", "/absolute", ".git/config", "nested/.GIT/config", "link", "link/child", "target/child"} {
		_, err := repo.EditTree(t.Context(), base, []git.FileEdit{{Path: path, After: []byte("x"), Mode: 0o100644}})
		require.Error(t, err, path)
	}
	for _, edits := range [][]git.FileEdit{
		{{Path: "new", Mode: 0o100644}, {Path: "new", Mode: 0o100644}},
		{{Path: "new", Mode: 0o100644}, {Path: "new/child", Mode: 0o100644}},
		{{Path: "new", Mode: 0o120000}},
		{{Path: "new", Delete: true}},
		{{Path: "new", Before: git.FileState{Mode: 0o100644}, Mode: 0o100644}},
	} {
		result, err := repo.EditTree(t.Context(), base, edits)
		require.Error(t, err)
		require.Empty(t, result)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := repo.EditTree(ctx, base, nil)
	require.ErrorIs(t, err, context.Canceled)
}
