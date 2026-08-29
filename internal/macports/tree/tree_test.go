package tree

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

func fakeTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, macports.PortGroupDir), 0o755))
	for _, p := range []string{"devel/foo", "devel/bar", "math/ivy"} {
		dir := filepath.Join(root, p)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, macports.PortfileName), []byte("PortSystem 1.0\n"), 0o644))
	}
	// Distractors: hidden and underscore dirs, a portless dir, a stray file.
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "devel", "empty"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "README"), nil, 0o644))
	return root
}

func TestOpenValidates(t *testing.T) {
	_, err := Open(t.TempDir())
	require.ErrorIs(t, err, ErrNotPortsTree)

	tr, err := Open(fakeTree(t))
	require.NoError(t, err)
	require.NotEmpty(t, tr.Root())
}

func TestPortdirs(t *testing.T) {
	tr, err := Open(fakeTree(t))
	require.NoError(t, err)
	dirs, err := tr.Portdirs()
	require.NoError(t, err)
	require.Len(t, dirs, 3)
	for _, d := range dirs {
		require.FileExists(t, filepath.Join(d, macports.PortfileName))
	}
}

func TestCategoryAndLookup(t *testing.T) {
	tr, err := Open(fakeTree(t))
	require.NoError(t, err)

	require.True(t, tr.HasCategory("devel"))
	require.False(t, tr.HasCategory("_resources"))
	require.False(t, tr.HasCategory("nope"))

	dirs, err := tr.CategoryPortdirs("devel")
	require.NoError(t, err)
	require.Len(t, dirs, 2)

	dir, err := tr.Lookup("ivy")
	require.NoError(t, err)
	require.Contains(t, dir, "math")

	_, err = tr.Lookup("nonexistent")
	require.ErrorIs(t, err, ErrPortNotFound)
}
