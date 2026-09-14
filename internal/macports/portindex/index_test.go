package portindex

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

var testPlatform = record.Platform{OS: "darwin", Version: "25", Architecture: "arm64"}

func TestPortIndexMirrorURLAndDownload(t *testing.T) {
	address, err := DefaultMirrorURL(testPlatform)
	require.NoError(t, err)
	require.Equal(t, "https://ftp.fau.de/macports/release/tarballs/PortIndex_darwin_25_arm64/PortIndex", address)
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/PortIndex", request.URL.Path)
		_, _ = response.Write([]byte("fixture index\n"))
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), portIndexName)
	require.NoError(t, downloadPortIndex(t.Context(), server.Client(), server.URL+"/PortIndex", destination))
	data, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.Equal(t, "fixture index\n", string(data))
}

func TestPortIndexUsesPortGroupsFromFrozenSource(t *testing.T) {
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
	require.NoError(t, buildPortIndex(t.Context(), config, testPlatform, root, destination, "", nil, true))
	data, err := os.ReadFile(filepath.Join(destination, portIndexName))
	require.NoError(t, err)
	require.Contains(t, string(data), "version 7.3")
	put("devel/index-fixture/Portfile", "PortSystem 1.0\nPortGroup dockhand-index 1.0\nname index-fixture\ncategories devel\nrevision 4\n")
	updated := filepath.Join(t.TempDir(), "index")
	require.NoError(t, buildPortIndex(t.Context(), config, testPlatform, root, updated, destination, []string{"devel/index-fixture/Portfile"}, true))
	data, err = os.ReadFile(filepath.Join(updated, portIndexName))
	require.NoError(t, err)
	require.Contains(t, string(data), "version 7.3")
	require.Contains(t, string(data), "revision 4")
}

func TestSharedPortGroupChangesRequireFullIndex(t *testing.T) {
	require.True(t, requiresFullIndex([]string{"_resources/port1.0/group/github-1.0.tcl"}))
	require.False(t, requiresFullIndex([]string{"devel/fixture/files/metadata.tcl"}))
}

func TestIncrementalIndexAllowsOnlyExistingUnrelatedOmissions(t *testing.T) {
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
	seed := filepath.Join(t.TempDir(), "seed")
	require.NoError(t, buildPortIndex(t.Context(), config, testPlatform, root, seed, "", nil, false))

	put("devel/working/Portfile", good+"revision 1\n")
	candidate := filepath.Join(t.TempDir(), "candidate")
	require.NoError(t, buildPortIndex(t.Context(), config, testPlatform, root, candidate, seed, []string{"devel/working/Portfile"}, true))
	indexed, err := Open(candidate)
	require.NoError(t, err)
	value, err := indexed.Lookup("working")
	require.NoError(t, err)
	require.Equal(t, "1", value.Fields["revision"])
	_, err = indexed.Lookup("broken")
	require.ErrorIs(t, err, ErrNotIndexed)

	put("devel/broken/Portfile", "PortSystem 1.0\nerror {changed port failure}\n")
	require.Error(t, buildPortIndex(t.Context(), config, testPlatform, root, filepath.Join(t.TempDir(), "changed-broken"), seed, []string{"devel/broken/Portfile"}, true))
	put("devel/working/Portfile", good+"subport child { error {new subport failure} }\n")
	require.Error(t, buildPortIndex(t.Context(), config, testPlatform, root, filepath.Join(t.TempDir(), "broken-subport"), seed, []string{"devel/working/Portfile"}, true))

	require.NoError(t, os.Remove(filepath.Join(root, "devel/working/Portfile")))
	require.Error(t, buildPortIndex(t.Context(), config, testPlatform, root, filepath.Join(t.TempDir(), "lost-unchanged"), seed, nil, true))
	require.NoError(t, buildPortIndex(t.Context(), config, testPlatform, root, filepath.Join(t.TempDir(), "removed"), seed, []string{"devel/working/Portfile"}, true))
	require.Error(t, buildPortIndex(t.Context(), config, testPlatform, root, filepath.Join(t.TempDir(), "full-strict"), "", nil, true))
}
