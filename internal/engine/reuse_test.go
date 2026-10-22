package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

// A port directory whose trees differ only in Portfile lines inside one
// subport's block names that subport, its siblings' results standing; any
// other file changed, or a change outside every block, says nothing
// (reuse.Blocks; field testing's terraform replay, 2026-10-22).
func TestADirectorysEditIsReadByTheSubportBlocksItIsIn(t *testing.T) {
	t.Parallel()
	f := setup(t)
	e := f.open(t)
	revision := harborBranch(t, e)
	portfile := func(v16, top string) string {
		return "PortSystem 1.0\nname terraform\nversion " + top + "\n\nsubport terraform-1.16 {\n    version " + v16 + "\n}\n\nsubport terraform-1.0 {\n    version 1.0.11\n}\n"
	}
	base := editTree(t, e, revision.Source.Tree, map[string]string{"sysutils/terraform/Portfile": portfile("1.16.6", "1.0"), "sysutils/terraform/files/a.patch": "a\n"})
	directory := func(tree model.ObjectID) model.ObjectID {
		t.Helper()
		objects, err := e.Repo.Directories(t.Context(), string(tree), []string{"sysutils/terraform"})
		require.NoError(t, err)
		return model.ObjectID(objects["sysutils/terraform"])
	}
	d := &driver{e: e}
	then := directory(base)

	subports, ok := d.subportEdits(t.Context(), then, directory(editTree(t, e, base, map[string]string{"sysutils/terraform/Portfile": portfile("1.16.7", "1.0")})))
	require.True(t, ok)
	require.Equal(t, []string{"terraform-1.16"}, subports)

	_, ok = d.subportEdits(t.Context(), then, directory(editTree(t, e, base, map[string]string{"sysutils/terraform/Portfile": portfile("1.16.6", "2.0")})))
	require.False(t, ok, "outside every block")
	_, ok = d.subportEdits(t.Context(), then, directory(editTree(t, e, base, map[string]string{"sysutils/terraform/files/a.patch": "b\n"})))
	require.False(t, ok, "another file changed")
	_, ok = d.subportEdits(t.Context(), then, directory(editTree(t, e, base, map[string]string{"sysutils/terraform/files/b.patch": "b\n"})))
	require.False(t, ok, "a file added")

	// The reading reuse takes, the same for a pair of trees asked again.
	blocks := d.blocks(t.Context())
	now := directory(editTree(t, e, base, map[string]string{"sysutils/terraform/Portfile": portfile("1.16.7", "1.0")}))
	for range 2 {
		subports, ok := blocks("sysutils/terraform", then, now)
		require.True(t, ok)
		require.Equal(t, []string{"terraform-1.16"}, subports)
	}
}
