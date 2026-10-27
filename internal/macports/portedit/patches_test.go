package portedit

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/distfetch"
	"github.com/herbygillot/dockhand/internal/macports/eval"
	"github.com/herbygillot/dockhand/internal/macports/portfile"
	"github.com/herbygillot/dockhand/internal/model"
	"github.com/herbygillot/dockhand/internal/scratch"
	"github.com/herbygillot/dockhand/internal/testsupport"
	"github.com/stretchr/testify/require"
)

// Serial: it asserts that no kept-archive directory survives in the system
// temp directory, which a parallel preparation of another fixture would hold
// open while running.
func TestPreparationChecksDeclaredPatchesAgainstTheNewSource(t *testing.T) {
	executable := testsupport.MacPortsTclsh(t)
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	body := "one\ntwo\nthree\n"
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "fixture-1.2.4/dir/a.txt", Mode: 0600, Size: int64(len(body)), Typeflag: tar.TypeReg}))
	_, _ = tw.Write([]byte(body))
	// A file whose lines moved down, which moved.diff patches where they were.
	moved := "x\ny\nz\none\ntwo\nthree\n"
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "fixture-1.2.4/dir/b.txt", Mode: 0600, Size: int64(len(moved)), Typeflag: tar.TypeReg}))
	_, _ = tw.Write([]byte(moved))
	// And one whose lines changed around a hunk, which fuzzy.diff patches
	// only with fuzz.
	fuzzed := "x\ny\nz\nONE\ntwo\nthree\nfour\nFIVE\n"
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "fixture-1.2.4/dir/c.txt", Mode: 0600, Size: int64(len(fuzzed)), Typeflag: tar.TypeReg}))
	_, _ = tw.Write([]byte(fuzzed))
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(archive.Bytes()) }))
	t.Cleanup(server.Close)
	root := t.TempDir()
	portdir := filepath.Join(root, "devel/fixture")
	require.NoError(t, os.MkdirAll(filepath.Join(portdir, "files"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(portdir, "files/good.diff"), []byte("--- dir/a.txt\n+++ dir/a.txt\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(portdir, "files/stale.diff"), []byte("--- dir/a.txt\n+++ dir/a.txt\n@@ -1,3 +1,3 @@\n uno\n-dos\n+DOS\n tres\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(portdir, "files/moved.diff"), []byte("--- dir/b.txt\n+++ dir/b.txt\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(portdir, "files/fuzzy.diff"), []byte("--- dir/c.txt\n+++ dir/c.txt\n@@ -4,5 +4,5 @@\n one\n two\n-three\n+THREE\n four\n five\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(portdir, "Portfile"), []byte(strings.ReplaceAll(`PortSystem 1.0
name fixture
categories devel
version 1.2.3
revision 1
master_sites @SITE@/${version}
checksums sha256 aaaa size 2
patchfiles good.diff stale.diff moved.diff fuzzy.diff
`, "@SITE@", server.URL)), 0600))
	s := &Service{Ports: &eval.Evaluator{Executable: executable, Adapter: testsupport.BaseAdapter()}, Archives: distfetch.Client{HTTP: server.Client()}}
	r := Request{Action: model.EditUpdate, Source: model.Source{Tree: model.ObjectID(strings.Repeat("a", 40))}, Workspace: adopt(t, root), Selection: macports.Selection{Selector: "fixture"}, Version: "1.2.4", Release: &model.Release{ReleaseSelection: model.ReleaseSelection{Requested: "1.2.4"}, Archive: true, Version: "1.2.4"}}
	result, err := s.Prepare(t.Context(), r)
	require.NoError(t, err, "a rejected patch is a finding, not a refusal")
	require.Contains(t, string(result.Files[0].After), "version 1.2.4\nrevision 0")
	require.Len(t, result.Patches, 4)
	require.Equal(t, "good.diff", result.Patches[0].Name)
	require.True(t, result.Patches[0].Applies)
	require.Equal(t, "stale.diff", result.Patches[1].Name)
	require.True(t, result.Patches[1].Checked)
	require.False(t, result.Patches[1].Applies)
	// Apple's patch says "hunks failed" and GNU patch "hunk FAILED".
	require.Regexp(t, `(?i)1 out of 1 hunks? failed`, result.Patches[1].Detail)
	// One at an offset is refreshed to apply as written, beside the
	// Portfile, which is as the update wrote it (the reflections' B).
	require.Equal(t, "applies as refreshed: hunk 1 of dir/b.txt moved to where it now applies", result.Patches[2].Detail)
	require.Equal(t, []RefreshedPatch{{Name: "moved.diff", Words: "hunk 1 of dir/b.txt moved to where it now applies"}}, result.Refreshed)
	refreshed := slices.IndexFunc(result.Files, func(f portfile.Edit) bool { return strings.HasSuffix(f.Path, "files/moved.diff") })
	require.GreaterOrEqual(t, refreshed, 0)
	require.Equal(t, "--- dir/b.txt\n+++ dir/b.txt\n@@ -2,5 +2,5 @@\n y\n z\n one\n-two\n+TWO\n three\n", string(result.Files[refreshed].After))
	require.Contains(t, string(result.portfileAfter()), "version 1.2.4\nrevision 0")
	// Nobody's update holds the fuzzed patch, unwritten (patch-fuzz).
	require.Regexp(t, `^applies only with fuzz: hunk 1 with fuzz \d`, result.Patches[3].Detail)
	require.False(t, slices.ContainsFunc(result.Files, func(f portfile.Edit) bool { return strings.HasSuffix(f.Path, "files/fuzzy.diff") }))

	// A person's re-emits it where patch put it, its fuzzed hunk said for
	// them to look over in the diff.
	r.Attended = true
	result, err = s.Prepare(t.Context(), r)
	require.NoError(t, err)
	require.Len(t, result.Refreshed, 2)
	require.Equal(t, "fuzzy.diff", result.Refreshed[1].Name)
	require.Regexp(t, `^hunk 1 of dir/c.txt with fuzz \d written where patch put it: look it over in the diff$`, result.Refreshed[1].Words)
	require.True(t, slices.ContainsFunc(result.Files, func(f portfile.Edit) bool { return strings.HasSuffix(f.Path, "files/fuzzy.diff") }))
	root, err = scratch.Root()
	require.NoError(t, err)
	matches, _ := filepath.Glob(filepath.Join(root, "patchcheck-*"))
	require.Empty(t, matches, "temporary archives and extractions are removed")
}
