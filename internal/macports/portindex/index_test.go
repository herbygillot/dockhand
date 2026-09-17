package portindex

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

var testPlatform = record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}

func testGeneration() generation { return generation{Tree: strings.Repeat("a", 40)} }

func TestPortIndexMirrorURL(t *testing.T) {
	t.Parallel()
	address, err := DefaultMirrorURL(testPlatform)
	require.NoError(t, err)
	require.Equal(t, "https://ftp.fau.de/macports/release/tarballs/PortIndex_darwin_25_arm64/PortIndex", address)
	_, err = DefaultMirrorURL(record.Platform{OS: "darwin"})
	require.Error(t, err)
}

func TestResolveToolProbesRuntimeOnlyForTclLaunchers(t *testing.T) {
	t.Parallel()
	stub := filepath.Join(t.TempDir(), "portindex")
	require.NoError(t, os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0700))
	config, err := ResolveTool(t.Context(), Config{Executable: stub})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(config.Digest, "sha256:"))
	require.Empty(t, config.Runtime)
	_, err = ResolveTool(t.Context(), Config{Executable: stub, Digest: "sha256:other"})
	require.ErrorContains(t, err, "executable changed")

	executable, err := exec.LookPath("portindex")
	if err != nil {
		t.Skip("MacPorts portindex is required for the runtime probe")
	}
	config, err = ResolveTool(t.Context(), Config{Executable: executable})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(config.Runtime, "macports-"), config.Runtime)
}

func TestPortIndexUsesPortGroupsFromFrozenSource(t *testing.T) {
	t.Parallel()
	executable, err := exec.LookPath("portindex")
	if err != nil {
		t.Skip("MacPorts portindex is required for integration test")
	}
	root := t.TempDir()
	put := func(name, contents string) {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, []byte(contents), 0600))
	}
	put("_resources/port1.0/group/dockhand-index-1.0.tcl", "version 7.3\n")
	put("devel/index-fixture/Portfile", "PortSystem 1.0\nPortGroup dockhand-index 1.0\nname index-fixture\ncategories devel\n")
	config, err := ResolveTool(t.Context(), Config{Executable: executable})
	require.NoError(t, err)
	destination := filepath.Join(t.TempDir(), "index")
	require.NoError(t, buildPortIndex(t.Context(), config, testPlatform, root, destination, "", nil, true, nil, testGeneration()))
	data, err := os.ReadFile(filepath.Join(destination, portIndexName))
	require.NoError(t, err)
	require.Contains(t, string(data), "version 7.3")
	meta, ok := readGeneration(destination)
	require.True(t, ok)
	require.True(t, meta.Full)
	require.True(t, meta.Strict)
	put("devel/index-fixture/Portfile", "PortSystem 1.0\nPortGroup dockhand-index 1.0\nname index-fixture\ncategories devel\nrevision 4\n")
	updated := filepath.Join(t.TempDir(), "index")
	require.NoError(t, buildPortIndex(t.Context(), config, testPlatform, root, updated, destination, []string{"devel/index-fixture/Portfile"}, true, nil, testGeneration()))
	data, err = os.ReadFile(filepath.Join(updated, portIndexName))
	require.NoError(t, err)
	require.Contains(t, string(data), "version 7.3")
	require.Contains(t, string(data), "revision 4")
	meta, ok = readGeneration(updated)
	require.True(t, ok)
	require.False(t, meta.Full)
	require.Equal(t, filepath.Base(destination), meta.Seed)
	require.Equal(t, 1, meta.Changed)
}

func TestSharedPortGroupChangesRequireFullIndex(t *testing.T) {
	t.Parallel()
	require.True(t, requiresFullIndex([]string{"_resources/port1.0/group/github-1.0.tcl"}))
	require.False(t, requiresFullIndex([]string{"devel/fixture/files/metadata.tcl"}))
}

func TestIncrementalIndexAllowsOnlyExistingUnrelatedOmissions(t *testing.T) {
	t.Parallel()
	executable, err := exec.LookPath("portindex")
	if err != nil {
		t.Skip("MacPorts portindex is required")
	}
	root := t.TempDir()
	put := func(name, contents string) {
		file := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(file), 0700))
		require.NoError(t, os.WriteFile(file, []byte(contents), 0600))
	}
	good := "PortSystem 1.0\nname working\nversion 1\ncategories devel\n"
	put("devel/working/Portfile", good)
	put("devel/spare/Portfile", "PortSystem 1.0\nname spare\nversion 1\ncategories devel\n")
	put("devel/broken/Portfile", "PortSystem 1.0\nerror {unrelated existing failure}\n")
	config, err := ResolveTool(t.Context(), Config{Executable: executable})
	require.NoError(t, err)
	build := func(destination, seed string, changed []string, strict bool) error {
		return buildPortIndex(t.Context(), config, testPlatform, root, destination, seed, changed, strict, nil, testGeneration())
	}
	seed := filepath.Join(t.TempDir(), "seed")
	require.NoError(t, build(seed, "", nil, false))

	put("devel/working/Portfile", good+"revision 1\n")
	candidate := filepath.Join(t.TempDir(), "candidate")
	require.NoError(t, build(candidate, seed, []string{"devel/working/Portfile"}, true))
	indexed, err := Open(candidate)
	require.NoError(t, err)
	value, err := indexed.Lookup("working")
	require.NoError(t, err)
	require.Equal(t, "1", value.Fields["revision"])
	_, err = indexed.Lookup("broken")
	require.ErrorIs(t, err, ErrNotIndexed)

	put("devel/broken/Portfile", "PortSystem 1.0\nerror {changed port failure}\n")
	require.Error(t, build(filepath.Join(t.TempDir(), "changed-broken"), seed, []string{"devel/broken/Portfile"}, true))
	put("devel/working/Portfile", good+"subport child { error {new subport failure} }\n")
	require.Error(t, build(filepath.Join(t.TempDir(), "broken-subport"), seed, []string{"devel/working/Portfile"}, true))

	require.NoError(t, os.Remove(filepath.Join(root, "devel/working/Portfile")))
	require.Error(t, build(filepath.Join(t.TempDir(), "lost-unchanged"), seed, nil, true))
	require.NoError(t, build(filepath.Join(t.TempDir(), "removed"), seed, []string{"devel/working/Portfile"}, true))
	require.Error(t, build(filepath.Join(t.TempDir(), "full-strict"), "", nil, true))
}
