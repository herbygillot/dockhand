package portindex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herbygillot/dockhand/internal/filelock"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestCacheRetentionSkipsBusyProfilesAndPreservesRecentOrUnknownPaths(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, strings.Repeat("a", 64))
	lock, err := filelock.Acquire(t.Context(), filepath.Join(profile, "index.lock"), filelock.Exclusive)
	require.NoError(t, err)
	t.Cleanup(func() { lock.Close() })
	old := time.Now().Add(-30 * 24 * time.Hour)
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	seed := strings.Repeat("b", 40)
	tree := strings.Repeat("c", 40)
	var entries []string
	for _, name := range []string{seed, "complete/" + tree, "standalone/" + tree, "candidates/" + seed + "/" + tree} {
		path := filepath.Join(profile, name)
		require.NoError(t, os.MkdirAll(path, 0700))
		require.NoError(t, os.WriteFile(filepath.Join(path, "PortIndex"), []byte("preserved content"), 0600))
		require.NoError(t, os.Chtimes(path, old, old))
		entries = append(entries, path)
	}
	recent := filepath.Join(profile, "complete", strings.Repeat("d", 40))
	unknown := filepath.Join(profile, "unexpected")
	for _, path := range []string{recent, unknown} {
		require.NoError(t, os.MkdirAll(path, 0700))
	}
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "keep"), nil, 0600))
	require.NoError(t, os.Symlink(outside, filepath.Join(profile, "complete", strings.Repeat("e", 40))))
	items, err := Collect(t.Context(), root, cutoff, false)
	require.NoError(t, err)
	require.Empty(t, items)
	require.NoError(t, lock.Close())
	items, err = Collect(t.Context(), root, cutoff, true)
	require.NoError(t, err)
	require.Len(t, items, 4)
	for _, path := range entries {
		require.DirExists(t, path)
	}
	items, err = Collect(t.Context(), root, cutoff, false)
	require.NoError(t, err)
	require.Len(t, items, 4)
	for _, item := range items {
		require.True(t, item.Completed)
		require.NoDirExists(t, item.Path)
	}
	require.DirExists(t, recent)
	require.DirExists(t, unknown)
	require.FileExists(t, filepath.Join(outside, "keep"))
	require.FileExists(t, filepath.Join(profile, "index.lock"))
	items, err = Collect(t.Context(), root, cutoff, false)
	require.NoError(t, err)
	require.Empty(t, items)
	absent := filepath.Join(root, "absent")
	_, err = Collect(t.Context(), absent, cutoff, false)
	require.NoError(t, err)
	require.NoDirExists(t, absent)
}

func TestStageRefreshesCacheAgeWithoutChangingIndexTimestamp(t *testing.T) {
	root := t.TempDir()
	cache := t.TempDir()
	tool := filepath.Join(t.TempDir(), "portindex")
	require.NoError(t, os.WriteFile(tool, []byte("#!/bin/sh\nexit 99\n"), 0700))
	c, err := ResolveTool(t.Context(), Config{Executable: tool, CacheDirectory: cache})
	require.NoError(t, err)
	profile := digest([]byte(c.Digest + "\x00\x00darwin\x0025\x00arm64"))
	tree := strings.Repeat("a", 40)
	entry := filepath.Join(cache, profile, "standalone", tree)
	require.NoError(t, os.MkdirAll(entry, 0700))
	old := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Second)
	for _, name := range []string{portIndexName, quickIndexName} {
		path := filepath.Join(entry, name)
		require.NoError(t, os.WriteFile(path, []byte("cached"), 0600))
		require.NoError(t, os.Chtimes(path, old, old))
	}
	require.NoError(t, os.Chtimes(entry, old, old))
	require.NoError(t, Stage(t.Context(), nil, record.Source{Tree: record.ObjectID(tree)}, testPlatform, c, root, nil))
	items, err := Collect(t.Context(), cache, time.Now().Add(-24*time.Hour), false)
	require.NoError(t, err)
	require.Empty(t, items)
	info, err := os.Stat(filepath.Join(entry, portIndexName))
	require.NoError(t, err)
	require.Equal(t, old, info.ModTime())
}
