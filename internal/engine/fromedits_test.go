package engine

import (
	"testing"

	"github.com/herbygillot/dockhand/internal/git"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/stretchr/testify/require"
)

// fromEdits follows each file's mode with its blob: a person's change of a
// mode breaks the chain as a change of content does, and an edit from
// before modes were kept passes only where the head keeps the base's mode,
// or a plain file's where the base had the file not (#194, C6).
func TestFromEditsComparesModes(t *testing.T) {
	t.Parallel()
	e := setup(t).open(t)
	ctx := t.Context()
	branch, err := e.Start(ctx, StartRequest{Name: "modes"})
	require.NoError(t, err)
	trees, err := e.Repo.CommitTrees(ctx, []string{string(branch.Base)})
	require.NoError(t, err)
	base, err := e.Repo.EditTree(ctx, trees[string(branch.Base)], []git.FileEdit{
		{Path: "devel/foo/Portfile", After: []byte("name foo\nversion 1\n"), Mode: 0o100644},
		{Path: "devel/foo/files/run.sh", After: []byte("#!/bin/sh\n"), Mode: 0o100644},
	})
	require.NoError(t, err)
	blob := func(tree, path string) model.ObjectID {
		file, _, err := e.Repo.File(ctx, tree, path)
		require.NoError(t, err)
		return model.ObjectID(file.Blob)
	}
	head := func(portfileMode, scriptMode uint32) string {
		before, _, err := e.Repo.File(ctx, base, "devel/foo/Portfile")
		require.NoError(t, err)
		script, _, err := e.Repo.File(ctx, base, "devel/foo/files/run.sh")
		require.NoError(t, err)
		tree, err := e.Repo.EditTree(ctx, base, []git.FileEdit{
			{Path: "devel/foo/Portfile", Before: before, After: []byte("name foo\nversion 2\n"), Mode: portfileMode},
			{Path: "devel/foo/files/run.sh", Before: script, After: []byte("#!/bin/sh\n"), Mode: scriptMode},
		})
		require.NoError(t, err)
		return tree
	}
	plain := head(0o100644, 0o100755)
	paths := []string{"devel/foo/Portfile", "devel/foo/files/run.sh"}
	edit := func(portfileModes, scriptModes [2]string) []model.Edit {
		return []model.Edit{{Kind: model.EditUpdate, Port: "foo", Directory: "devel/foo", Subject: "foo: update to 2", Files: []model.EditedFile{
			{Path: "devel/foo/Portfile", Before: blob(base, "devel/foo/Portfile"), After: blob(plain, "devel/foo/Portfile"), BeforeMode: portfileModes[0], AfterMode: portfileModes[1]},
			{Path: "devel/foo/files/run.sh", Before: blob(base, "devel/foo/files/run.sh"), After: blob(plain, "devel/foo/files/run.sh"), BeforeMode: scriptModes[0], AfterMode: scriptModes[1]},
		}}}
	}
	recorded := edit([2]string{"100644", "100644"}, [2]string{"100644", "100755"})
	ok, _, err := fromEdits(ctx, e.Repo, base, plain, "devel/foo", paths, recorded)
	require.NoError(t, err)
	require.True(t, ok, "the head is the edit's, modes and all")

	ok, _, err = fromEdits(ctx, e.Repo, base, head(0o100755, 0o100755), "devel/foo", paths, recorded)
	require.NoError(t, err)
	require.False(t, ok, "a person made the Portfile executable")

	old := edit([2]string{}, [2]string{})
	ok, _, err = fromEdits(ctx, e.Repo, base, head(0o100644, 0o100644), "devel/foo", paths, old)
	require.NoError(t, err)
	require.True(t, ok, "an edit from before passes where the head keeps the base's modes")
	ok, _, err = fromEdits(ctx, e.Repo, base, plain, "devel/foo", paths, old)
	require.NoError(t, err)
	require.False(t, ok, "and not where a mode moved, which it can't show was dockhand's")

	created, err := e.Repo.EditTree(ctx, base, []git.FileEdit{{Path: "devel/foo/files/new.diff", After: []byte("x\n"), Mode: 0o100644}})
	require.NoError(t, err)
	added := []model.Edit{{Kind: model.EditUpdate, Port: "foo", Directory: "devel/foo", Subject: "foo: update to 2",
		Files: []model.EditedFile{{Path: "devel/foo/files/new.diff", After: blob(created, "devel/foo/files/new.diff")}}}}
	ok, _, err = fromEdits(ctx, e.Repo, base, created, "devel/foo", []string{"devel/foo/files/new.diff"}, added)
	require.NoError(t, err)
	require.True(t, ok, "a file an old edit added is a plain file")
}
