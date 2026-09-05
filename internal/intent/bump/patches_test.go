package bump

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/macports/info"
	"github.com/herbygillot/dockhand/internal/plan"
	"github.com/herbygillot/dockhand/internal/testenv"
	"github.com/herbygillot/dockhand/internal/tool"
)

// The patch the fixture port carries, written against a Makefile whose
// LDFLAGS line is the fourth: one hunk, three lines of before-block,
// at -p0 as base's default has it. The header carries diff's
// timestamps so that the rewrite is proven to keep them.
const (
	patchName = "patch-foo.diff"
	patchBody = "--- Makefile.orig\t2026-01-01 00:00:00.000000000 +0000\n" +
		"+++ Makefile\t2026-01-02 00:00:00.000000000 +0000\n" +
		"@@ -3,3 +3,3 @@\n" +
		" CFLAGS = -O2\n" +
		"-LDFLAGS = -lstdc++\n" +
		"+LDFLAGS =\n" +
		" all: prog\n"
	// makefile is the source the patch was written against: the
	// before-block starts on line 3.
	makefile = "# Makefile\nCC = cc\nCFLAGS = -O2\nLDFLAGS = -lstdc++\nall: prog\n\tcc -o prog prog.c\n"
)

// tarball builds a gzipped tarball in memory, the form a distfile
// arrives in, with members under the directory the port evaluates its
// source into.
func tarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

// sourceServer serves a real tarball for the new version's distfile —
// the relocation has to read a Makefile out of it — and distServer's
// derived bytes for anything else.
func sourceServer(t *testing.T, path string, archive []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == path {
			_, _ = w.Write(archive)
			return
		}
		_, _ = w.Write(servedFor(r.URL.Path))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// patchedPort is bumpPort with one patchfile under files/. The
// checksums recorded are for 1.0, which a bump never fetches; what
// matters is that they are there, so the fetch happens at all.
func patchedPort(t *testing.T, siteURL string) string {
	t.Helper()
	dir := bumpPort(t, siteURL, servedFor("/dist/bumpee-1.0.tar.gz"))
	portfile := filepath.Join(dir, macports.PortfileName)
	src, err := os.ReadFile(portfile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(portfile, append(src, []byte("patchfiles "+patchName+"\n")...), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "files"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "files", patchName), []byte(patchBody), 0o644))
	return dir
}

// planPatched runs a bump to 2.0 over the fixture port against a 2.0
// tarball carrying the given Makefile. The finder is the real one: the
// relocation reads the tarball through the host's tar, and the fixture
// is a real archive because the reader is real.
func planPatched(t *testing.T, newMakefile string) (*plan.Plan, error) {
	t.Helper()
	testenv.Tool(t, "tar")
	ev := newEvaluator(t)
	srv := sourceServer(t, "/dist/bumpee-2.0.tar.gz", tarball(t, map[string]string{
		"bumpee-2.0/Makefile": newMakefile,
		"bumpee-2.0/prog.c":   "int main(void) { return 0; }\n",
	}))
	dir := patchedPort(t, srv.URL+"/dist")
	return Bump{Version: "2.0", Tools: tool.NewFinder(nil)}.Plan(context.Background(), handle(dir, ev), newFetcher(t))
}

// Two lines were added above the hunk in 2.0. The plan carries the
// whole patch beside the Portfile with only its @@ numbers rewritten,
// and the subject says so.
func TestBumpRelocatesAPatchWhoseHunkMoved(t *testing.T) {
	p, err := planPatched(t, "# bumpee 2.0\n# now with a header\n"+makefile)
	require.NoError(t, err)

	require.Len(t, p.Files, 1)
	f := p.Files[0]
	assert.Equal(t, "files/"+patchName, f.Path)
	assert.Equal(t, "1 hunk moved", f.Reason)
	assert.Equal(t, strings.Replace(patchBody, "@@ -3,3 +3,3 @@", "@@ -5,3 +5,3 @@", 1), f.Content,
		"the @@ line moves and every other byte stays")
	assert.Equal(t, "bumpee: update to 2.0, refresh "+patchName, p.Summary)
	assert.Equal(t, "bumpee-2.0", p.Slug, "the slug is the version's, not the patch's")

	// The Portfile's own edits are what they always were: the patch
	// rides beside them and displaces nothing.
	reasons := make(map[string]int)
	for _, e := range p.Edits {
		reasons[e.Reason]++
	}
	assert.Equal(t, 1, reasons["version"])
	assert.Equal(t, 1, reasons["checksum sha256"])
}

// The hunk is where it was: nothing to write, and the subject does not
// mention a patch that did not move.
func TestBumpLeavesAPatchThatStillApplies(t *testing.T) {
	p, err := planPatched(t, makefile)
	require.NoError(t, err)
	assert.Empty(t, p.Files)
	assert.Equal(t, "bumpee: update to 2.0", p.Summary)
}

// The line the patch removes is gone from 2.0, so the before-block
// occurs nowhere: the bump declines, naming the patch, the file and
// the hunk as patch(1) would number it.
func TestBumpDeclinesAPatchWhoseHunkIsGone(t *testing.T) {
	p, err := planPatched(t, strings.Replace(makefile, "LDFLAGS = -lstdc++", "LDFLAGS = -lc++", 1))
	require.Nil(t, p)
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.PatchWontRelocate, d.Type)
	assert.Equal(t, "files/patch-foo.diff: Makefile hunk #1: its before-block occurs nowhere in the file", d.Detail)
	assert.Contains(t, err.Error(), "refresh the patch by hand")
	// A server's answer and a file under files/ decided this, neither
	// of which the memo's key holds.
	assert.Equal(t, plan.ByNetwork, d.Determined)
	assert.False(t, d.Memoizable())
}

// The give-ups that need no evaluator: what the helper says when the
// patch itself, rather than a hunk, is the reason. The fetched source
// is a real tarball because the reader is real even when it is never
// reached.
func TestRelocatePatchesGivesUpOnThePatchItself(t *testing.T) {
	testenv.Tool(t, "tar")
	archive := filepath.Join(t.TempDir(), "bumpee-2.0.tar.gz")
	require.NoError(t, os.WriteFile(archive, tarball(t, map[string]string{"bumpee-2.0/Makefile": makefile}), 0o644))

	for _, tc := range []struct {
		name  string
		files map[string]string // under files/
		vals  info.Values
		want  string // the decline's detail
	}{
		{
			name: "not in the portdir",
			vals: info.Values{Patchfiles: []string{patchName}},
			want: "files/patch-foo.diff is not in the portdir; a patch the port fetches from patch_sites is not dockhand's to refresh",
		},
		{
			name:  "a fetch tag is not part of the name",
			files: map[string]string{"other.diff": patchBody},
			vals:  info.Values{Patchfiles: []string{patchName + ":tag"}},
			want:  "files/patch-foo.diff is not in the portdir; a patch the port fetches from patch_sites is not dockhand's to refresh",
		},
		{
			name:  "not a unified diff",
			files: map[string]string{patchName: "*** Makefile.orig\n--- Makefile\n***************\n"},
			vals:  info.Values{Patchfiles: []string{patchName}},
			want:  "files/patch-foo.diff: patch: not a unified diff: no ---/+++ header",
		},
		{
			name:  "the target is not in the distfile",
			files: map[string]string{patchName: strings.ReplaceAll(patchBody, "Makefile", "GNUmakefile")},
			vals:  info.Values{Patchfiles: []string{patchName}},
			want:  "files/patch-foo.diff: GNUmakefile hunk #1: the file could not be read: distfile: file not found in any distfile: bumpee-2.0/GNUmakefile, tried 1 (bumpee-2.0.tar.gz: distfile: file not found in any distfile)",
		},
		{
			name:  "the strip level is the port's",
			files: map[string]string{patchName: strings.ReplaceAll(patchBody, "Makefile", "a/Makefile")},
			vals:  info.Values{Patchfiles: []string{patchName}, PatchPreArgs: "-p1"},
			want:  "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			portdir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(portdir, "files"), 0o755))
			for name, body := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(portdir, "files", name), []byte(body), 0o644))
			}
			files, moved, err := relocatePatches(context.Background(), tool.NewFinder(nil), portdir, tc.vals, "bumpee-2.0", []string{archive})
			if tc.want == "" {
				require.NoError(t, err)
				assert.Empty(t, files, "the hunk is where it was")
				assert.Empty(t, moved)
				return
			}
			var d *plan.Decline
			require.ErrorAs(t, err, &d)
			assert.Equal(t, plan.PatchWontRelocate, d.Type)
			assert.Equal(t, tc.want, d.Detail)
			assert.Equal(t, plan.ByNetwork, d.Determined)
			assert.Nil(t, files)
		})
	}
}

// The target is the file the patch phase opens and no other. The new
// release dropped its top-level Makefile and kept one under src/ that
// carries the very lines the hunk expects; distfile.Extract's
// shallowest-copy fallback would read that one, and the relocation
// would then describe a file patch(1) at -p0 in the worksrcdir will
// never see. Reading the exact member instead makes this the decline
// it should be.
func TestRelocatePatchesReadsOnlyTheFileThePatchPhaseOpens(t *testing.T) {
	testenv.Tool(t, "tar")
	archive := filepath.Join(t.TempDir(), "bumpee-2.0.tar.gz")
	require.NoError(t, os.WriteFile(archive, tarball(t, map[string]string{
		"bumpee-2.0/src/Makefile": makefile,
		"bumpee-2.0/configure":    "#!/bin/sh\n",
	}), 0o644))
	portdir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(portdir, "files"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(portdir, "files", patchName), []byte(patchBody), 0o644))

	files, moved, err := relocatePatches(context.Background(), tool.NewFinder(nil), portdir, info.Values{Patchfiles: []string{patchName}}, "bumpee-2.0", []string{archive})
	var d *plan.Decline
	require.ErrorAs(t, err, &d)
	assert.Equal(t, plan.PatchWontRelocate, d.Type)
	assert.Contains(t, d.Detail, "files/patch-foo.diff: Makefile hunk #1: the file could not be read")
	assert.Contains(t, d.Detail, "bumpee-2.0/Makefile")
	assert.Nil(t, files)
	assert.Nil(t, moved)
}

// No patchfiles: nothing is read, nothing is looked for, and the plan
// is the plan it always was. The command's goldens pin the same fact
// end to end — bump_plan.golden carries no "files" key and its summary
// is unchanged.
func TestRelocatePatchesDoesNothingWithoutPatchfiles(t *testing.T) {
	files, moved, err := relocatePatches(context.Background(), nil, t.TempDir(), info.Values{}, "bumpee-2.0", nil)
	require.NoError(t, err)
	assert.Nil(t, files)
	assert.Nil(t, moved)
}

func TestHunksMoved(t *testing.T) {
	assert.Equal(t, "1 hunk moved", hunksMoved(1))
	assert.Equal(t, "2 hunks moved", hunksMoved(2))
}
