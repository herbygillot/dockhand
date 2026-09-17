package portindex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/stretchr/testify/require"
)

func TestCacheRetentionSkipsBusyEnvironmentsAndPreservesSeedsRecentOrUnknownPaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	busy := filepath.Join(root, strings.Repeat("a", 64))
	lock, err := filelock.Acquire(t.Context(), filepath.Join(busy, cacheLockName), filelock.Shared)
	require.NoError(t, err)
	t.Cleanup(func() { lock.Close() })
	old := time.Now().Add(-30 * 24 * time.Hour)
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	seed := strings.Repeat("b", 40)
	tree := strings.Repeat("c", 40)
	latest := strings.Repeat("d", 40)
	recent := strings.Repeat("e", 40)
	mkdir := func(profile string, names ...string) []string {
		var paths []string
		for _, name := range names {
			path := filepath.Join(profile, name)
			require.NoError(t, os.MkdirAll(path, 0700))
			require.NoError(t, os.WriteFile(filepath.Join(path, "PortIndex"), []byte("content"), 0600))
			require.NoError(t, os.Chtimes(path, old, old))
			paths = append(paths, path)
		}
		return paths
	}
	busyEntries := mkdir(busy, generationsDirectory+"/"+tree)

	current := filepath.Join(root, strings.Repeat("f", 64))
	idle, err := filelock.Acquire(t.Context(), filepath.Join(current, cacheLockName), filelock.Shared)
	require.NoError(t, err)
	idle.Close()
	require.NoError(t, os.WriteFile(filepath.Join(current, latestFileName), []byte(latest+"\n"), 0600))
	removable := mkdir(current, generationsDirectory+"/"+tree, generationsDirectory+"/.index-stale")
	kept := mkdir(current, generationsDirectory+"/"+latest)
	require.NoError(t, os.MkdirAll(filepath.Join(current, generationsDirectory, recent), 0700))
	unknown := filepath.Join(current, "unexpected")
	require.NoError(t, os.MkdirAll(unknown, 0700))
	require.NoError(t, os.Chtimes(unknown, old, old))

	legacy := filepath.Join(root, strings.Repeat("0", 64))
	legacyLock, err := filelock.Acquire(t.Context(), filepath.Join(legacy, "index.lock"), filelock.Exclusive)
	require.NoError(t, err)
	legacyLock.Close()
	legacyEntries := mkdir(legacy, seed, "complete/"+tree, "standalone/"+tree, "candidates/"+seed+"/"+tree)

	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "keep"), nil, 0600))
	require.NoError(t, os.Symlink(outside, filepath.Join(current, generationsDirectory, strings.Repeat("9", 40))))

	preview, err := Collect(t.Context(), root, cutoff, true)
	require.NoError(t, err)
	var previewed []string
	for _, item := range preview {
		require.False(t, item.Completed)
		previewed = append(previewed, item.Path)
	}
	require.ElementsMatch(t, append(append([]string{}, removable...), legacyEntries...), previewed)
	for _, path := range append(append(removable, busyEntries...), legacyEntries...) {
		require.DirExists(t, path)
	}

	removed, err := Collect(t.Context(), root, cutoff, false)
	require.NoError(t, err)
	var completed []string
	for _, item := range removed {
		require.True(t, item.Completed)
		completed = append(completed, item.Path)
	}
	require.ElementsMatch(t, previewed, completed)
	for _, path := range append(removable, legacyEntries...) {
		require.NoDirExists(t, path)
	}
	for _, path := range append(append(kept, busyEntries...), unknown, filepath.Join(current, generationsDirectory, recent)) {
		require.DirExists(t, path)
	}
	require.FileExists(t, filepath.Join(outside, "keep"))

	missing, err := Collect(t.Context(), filepath.Join(root, "missing"), cutoff, false)
	require.NoError(t, err)
	require.Empty(t, missing)
	require.NoDirExists(t, filepath.Join(root, "missing"))
}
