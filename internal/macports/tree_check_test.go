package macports

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/model"
)

func TestValidatePortsTreeRequiresACategoryPortfile(t *testing.T) {
	root := t.TempDir()
	require.ErrorIs(t, ValidatePortsTree(root, ""), errNotPortsTree)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "_resources", "port1.0"), 0700))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "cmd", "tool"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "cmd", "tool", "main.go"), []byte("package main\n"), 0600))
	err := ValidatePortsTree(root, "/checkout")
	require.ErrorIs(t, err, errNotPortsTree)
	require.ErrorContains(t, err, "/checkout has no")
	require.ErrorContains(t, err, "--tree")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "devel", "fixture"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "devel", "fixture", "Portfile"), []byte("PortSystem 1.0\n"), 0600))
	require.NoError(t, ValidatePortsTree(root, ""))
	require.ErrorIs(t, ValidatePortsTree(filepath.Join(root, "missing"), ""), os.ErrNotExist)
}

// A path that names no port is said as the tree's path, not as the
// snapshot's temporary directory (batch 14).
func TestSelectingAPathWithNoPortSaysTheTreesPath(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sysutils", "jq"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sysutils", "jq", "Portfile"), []byte("PortSystem 1.0\n"), 0600))
	tree, err := NewTree(model.Source{Commit: "c", Tree: "t"}, root, model.Platform{})
	require.NoError(t, err)
	_, err = tree.Select(model.Target{Name: "jq", Portfile: "textproc/jq/Portfile"})
	require.EqualError(t, err, "no port at textproc/jq")
	_, err = tree.Select(model.Target{Name: "jq", Portfile: "sysutils/jq/Portfile"})
	require.NoError(t, err)
}
