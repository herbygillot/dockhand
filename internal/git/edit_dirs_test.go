package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiffDirectoriesComparesAWithB(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string]string{"a/src/x.c": "a\nb\n", "b/src/x.c": "a\nc\n", "b/NEWS": "y\n", "a/same": "s\n", "b/same": "s\n"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(data), 0o644))
	}
	repo := &Repository{Root: root}
	patch, err := repo.DiffDirectories(t.Context(), root)
	require.NoError(t, err)
	require.Contains(t, string(patch), "+++ b/NEWS\n")
	require.Contains(t, string(patch), "--- a/src/x.c\n+++ b/src/x.c\n@@ -1,2 +1,2 @@\n a\n-b\n+c\n")
	require.False(t, strings.Contains(string(patch), "same"))

	require.NoError(t, os.WriteFile(filepath.Join(root, "b/src/x.c"), []byte("a\nb\n"), 0o644))
	require.NoError(t, os.Remove(filepath.Join(root, "b/NEWS")))
	patch, err = repo.DiffDirectories(t.Context(), root)
	require.NoError(t, err)
	require.Empty(t, patch)
}
