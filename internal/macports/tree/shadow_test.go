package tree

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

func TestCreateShadow(t *testing.T) {
	portdir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(portdir, macports.PortfileName), []byte("version 1.0\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(portdir, "files"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(portdir, "files", "patch-x.diff"), []byte("--- a\n+++ b\n"), 0o644))
	// A work symlink from a local build is not part of the port.
	require.NoError(t, os.Symlink("/nonexistent/build", filepath.Join(portdir, "work")))

	dir, err := Shadow(portdir, []byte("version 2.0\n"))
	require.NoError(t, err)
	defer os.RemoveAll(dir) //nolint:errcheck

	pf, err := os.ReadFile(filepath.Join(dir, macports.PortfileName))
	require.NoError(t, err)
	assert.Equal(t, "version 2.0\n", string(pf), "Portfile carries the replacement bytes")

	patch, err := os.ReadFile(filepath.Join(dir, "files", "patch-x.diff"))
	require.NoError(t, err)
	assert.Equal(t, "--- a\n+++ b\n", string(patch), "files/ rides along")

	_, err = os.Lstat(filepath.Join(dir, "work"))
	assert.True(t, os.IsNotExist(err), "symlinks are skipped")
}
