package buildenv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every staged tree under a check's directory is removed, its bytes
// counted, the logs beside them left; a directory that isn't there
// removes nothing.
func TestStagedTreesAreRemoved(t *testing.T) {
	t.Parallel()
	check := t.TempDir()
	for _, attempt := range []string{"tart-26-arm64-1", "tart-26-arm64-2"} {
		require.NoError(t, os.MkdirAll(filepath.Join(check, attempt), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(check, attempt, StagedTree), []byte("tree"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(check, attempt, "target-1.log"), []byte("log"), 0o644))
	}
	count, bytes, err := RemoveStagedTrees(check)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.EqualValues(t, 8, bytes)
	require.NoFileExists(t, filepath.Join(check, "tart-26-arm64-1", StagedTree))
	require.FileExists(t, filepath.Join(check, "tart-26-arm64-1", "target-1.log"))
	count, _, err = RemoveStagedTrees(filepath.Join(check, "nosuch"))
	require.NoError(t, err)
	require.Zero(t, count)
}
