package atomicfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteReplacesContentAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "result")
	require.NoError(t, Write(path, []byte("old"), 0644))
	require.NoError(t, Write(path, []byte("new"), 0600))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "new", string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestFailedReplacementRemovesTemporaryFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "existing-directory")
	require.NoError(t, os.Mkdir(path, 0700))
	require.Error(t, Write(path, []byte("cannot replace directory"), 0600))
	require.DirExists(t, path)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}
