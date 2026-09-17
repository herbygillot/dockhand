package macports

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
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
