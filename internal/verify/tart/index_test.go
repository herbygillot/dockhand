package tart

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPortIndexMirrorURLAndDownload(t *testing.T) {
	address, err := defaultPortIndexURL(testPlatform)
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
	config, err := resolvePortIndexTool(t.Context(), Config{PortIndexExecutable: executable})
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
