package portedit

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/stretchr/testify/require"
)

func TestPreparationChecksDeclaredPatchesAgainstTheNewSource(t *testing.T) {
	executable, err := exec.LookPath("port-tclsh")
	if err != nil {
		t.Skip("MacPorts required")
	}
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	body := "one\ntwo\nthree\n"
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "fixture-1.2.4/dir/a.txt", Mode: 0600, Size: int64(len(body)), Typeflag: tar.TypeReg}))
	_, _ = tw.Write([]byte(body))
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive.Bytes()) }))
	t.Cleanup(server.Close)
	root := t.TempDir()
	portdir := filepath.Join(root, "devel/fixture")
	require.NoError(t, os.MkdirAll(filepath.Join(portdir, "files"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(portdir, "files/good.diff"), []byte("--- dir/a.txt\n+++ dir/a.txt\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(portdir, "files/stale.diff"), []byte("--- dir/a.txt\n+++ dir/a.txt\n@@ -1,3 +1,3 @@\n uno\n-dos\n+DOS\n tres\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(portdir, "Portfile"), []byte(strings.ReplaceAll(`PortSystem 1.0
name fixture
categories devel
version 1.2.3
revision 1
master_sites @SITE@/${version}
checksums sha256 aaaa size 2
patchfiles good.diff stale.diff
`, "@SITE@", server.URL)), 0600))
	s := &Service{Ports: &eval.Evaluator{Executable: executable}, HTTP: server.Client()}
	r := Request{Action: record.Bump, Source: record.Source{Tree: record.ObjectID(strings.Repeat("a", 40))}, Root: root, Selection: macports.Selection{Selector: "fixture"}, Version: "1.2.4", Release: &record.Release{Selection: record.Selection{Requested: "1.2.4"}, Archive: true, Version: "1.2.4"}}
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err, "a rejected patch is a finding, not a refusal")
	require.Contains(t, string(result.Files[0].After), "version 1.2.4\nrevision 0")
	require.Len(t, result.Patches, 2)
	require.Equal(t, "good.diff", result.Patches[0].Name)
	require.True(t, result.Patches[0].Applies)
	require.Equal(t, "stale.diff", result.Patches[1].Name)
	require.True(t, result.Patches[1].Checked)
	require.False(t, result.Patches[1].Applies)
	require.Contains(t, result.Patches[1].Detail, "1 out of 1 hunks failed")
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "dockhand-patchcheck-*"))
	require.Empty(t, matches, "temporary archives and extractions are removed")
}
