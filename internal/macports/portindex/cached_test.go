package portindex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/filelock"
)

// cacheGeneration writes a completed generation of a tree into an
// environment of the cache root, as a build publishes one: its index, its
// quick index, its metadata, and the environment's lock.
func cacheGeneration(t *testing.T, directory, profile, tree string, entries map[string]string) {
	t.Helper()
	entry := filepath.Join(directory, profile, generationsDirectory, tree)
	require.NoError(t, os.MkdirAll(entry, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(directory, profile, cacheLockName), nil, 0o600))
	var index, quick strings.Builder
	for name, payload := range entries {
		fmt.Fprintf(&quick, "%s %d\n", name, index.Len())
		payload += "\n"
		fmt.Fprintf(&index, "%s %d\n%s", name, len(utf16.Encode([]rune(payload))), payload)
	}
	require.NoError(t, os.WriteFile(filepath.Join(entry, portIndexName), []byte(index.String()), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(entry, quickIndexName), []byte(quick.String()), 0o600))
	meta, err := json.Marshal(generation{Tree: tree})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(entry, generationFileName), meta, 0o600))
}

// A cached generation is found and read by its tree, in whichever
// environment has it; an absent cache, a tree none has, and a generation
// whose metadata names another tree are not found.
func TestReadCachedReadsOnlyWhatIsCached(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	profile := strings.Repeat("a", 64)
	tree, other := strings.Repeat("1", 40), strings.Repeat("2", 40)
	cacheGeneration(t, directory, profile, tree, map[string]string{"zlib": "name zlib portdir archivers/zlib maintainers {@ryandesign openmaintainer}"})

	trees, err := CachedTrees(directory)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{tree: true}, trees)
	var maintainers string
	found, err := ReadCached(t.Context(), directory, tree, func(index *Index) error {
		entry, err := index.Lookup("zlib")
		maintainers = entry.Fields["maintainers"]
		return err
	})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "@ryandesign openmaintainer", maintainers)

	for _, missing := range []struct{ directory, tree string }{{directory, other}, {filepath.Join(directory, "absent"), tree}} {
		found, err := ReadCached(t.Context(), missing.directory, missing.tree, func(*Index) error { t.Fatal("read"); return nil })
		require.NoError(t, err)
		require.False(t, found)
	}
	trees, err = CachedTrees(filepath.Join(directory, "absent"))
	require.NoError(t, err)
	require.Empty(t, trees)

	// Metadata naming another tree doesn't certify the files.
	meta, err := json.Marshal(generation{Tree: other})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, profile, generationsDirectory, tree, generationFileName), meta, 0o600))
	found, err = ReadCached(t.Context(), directory, tree, func(*Index) error { t.Fatal("read"); return nil })
	require.NoError(t, err)
	require.False(t, found)
}

// An environment a cleanup holds is passed over rather than waited on.
func TestReadCachedPassesOverAnEnvironmentBeingCleared(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	profile := strings.Repeat("b", 64)
	tree := strings.Repeat("3", 40)
	cacheGeneration(t, directory, profile, tree, map[string]string{"zlib": "name zlib portdir archivers/zlib maintainers nomaintainer"})
	guard, err := filelock.TryExisting(t.Context(), filepath.Join(directory, profile, cacheLockName), filelock.Exclusive)
	require.NoError(t, err)
	defer guard.Close()
	found, err := ReadCached(t.Context(), directory, tree, func(*Index) error { t.Fatal("read"); return nil })
	require.NoError(t, err)
	require.False(t, found)
}
