package patchcheck

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func sourceArchive(t *testing.T, files map[string]string) string {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(body)), Typeflag: tar.TypeReg}))
		_, _ = tw.Write([]byte(body))
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	file := filepath.Join(t.TempDir(), "source.tar.gz")
	require.NoError(t, os.WriteFile(file, data.Bytes(), 0600))
	return file
}

const good = "--- dir/a.txt\n+++ dir/a.txt\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n"
const stale = "--- dir/a.txt\n+++ dir/a.txt\n@@ -1,3 +1,3 @@\n uno\n-dos\n+DOS\n tres\n"
const missing = "--- dir/gone.txt\n+++ dir/gone.txt\n@@ -1,1 +1,1 @@\n-x\n+y\n"

func TestCheckReportsEachPatch(t *testing.T) {
	archive := sourceArchive(t, map[string]string{"project-2.0/dir/a.txt": "one\ntwo\nthree\n", "project-2.0/README": "unrelated\n"})
	var gzipped bytes.Buffer
	gz := gzip.NewWriter(&gzipped)
	_, _ = gz.Write([]byte(good))
	require.NoError(t, gz.Close())
	results, err := Check(t.Context(), Request{Archives: []string{archive}, Worksrcdir: "project-2.0", PreArgs: []string{"-t", "-N", "-p0"}, Patches: []Patch{
		{Name: "good.diff", Data: []byte(good)}, {Name: "stale.diff", Data: []byte(stale)}, {Name: "missing.diff", Data: []byte(missing)},
		{Name: "good.diff.gz", Data: gzipped.Bytes()}, {Name: "packed.diff.xz", Data: []byte("irrelevant")},
	}})
	require.NoError(t, err)
	require.Len(t, results, 5)
	require.Equal(t, Result{Name: "good.diff", Checked: true, Applies: true, Detail: "applies"}, results[0])
	require.True(t, results[1].Checked)
	require.False(t, results[1].Applies)
	require.Contains(t, results[1].Detail, "1 out of 1 hunks failed")
	require.False(t, results[2].Applies)
	require.Contains(t, results[2].Detail, "No file to patch")
	require.True(t, results[3].Applies, "gzip-compressed patches are decompressed")
	require.False(t, results[4].Checked)
	require.Contains(t, results[4].Detail, "xz compression is not modeled")
	require.Len(t, Rejected(results), 2)
	require.Contains(t, Summary(results), "stale.diff 1 out of 1 hunks failed")
	require.Contains(t, Summary(results), "packed.diff.xz unchecked")
}

func TestCheckHonorsStripRenameAndPatchDir(t *testing.T) {
	archive := sourceArchive(t, map[string]string{"upstream-name/sub/dir/a.txt": "one\ntwo\nthree\n"})
	p1 := strings.ReplaceAll(good, "--- dir/a.txt", "--- a/dir/a.txt")
	p1 = strings.ReplaceAll(p1, "+++ dir/a.txt", "+++ b/dir/a.txt")
	results, err := Check(t.Context(), Request{Archives: []string{archive}, Worksrcdir: "project-2.0/sub", Rename: true, PreArgs: []string{"-p1"}, Patches: []Patch{{Name: "p1.diff", Data: []byte(p1)}}})
	require.NoError(t, err)
	require.True(t, results[0].Applies, results[0].Detail)
	results, err = Check(t.Context(), Request{Archives: []string{archive}, Worksrcdir: "project-2.0/sub", PreArgs: []string{"-p1"}, Patches: []Patch{{Name: "p1.diff", Data: []byte(p1)}}})
	require.NoError(t, err)
	require.False(t, results[0].Applies, "without extract.rename a differing top-level directory is not the source")
	inner := strings.ReplaceAll(good, "dir/a.txt", "a.txt")
	results, err = Check(t.Context(), Request{Archives: []string{archive}, Worksrcdir: "upstream-name/sub", PatchDir: "dir", PreArgs: []string{"-p0"}, Patches: []Patch{{Name: "inner.diff", Data: []byte(inner)}}})
	require.NoError(t, err)
	require.True(t, results[0].Applies, results[0].Detail)
	results, err = Check(t.Context(), Request{Archives: []string{archive}, Worksrcdir: "upstream-name/sub", PreArgs: []string{"-p0", "--posix"}, Patches: []Patch{{Name: "good.diff", Data: []byte(good)}}})
	require.NoError(t, err)
	require.False(t, results[0].Checked)
	require.Contains(t, results[0].Detail, `"--posix" is not modeled`)
	require.Equal(t, "1 patches apply", Summary([]Result{{Name: "x", Checked: true, Applies: true}}))
}

func TestTargets(t *testing.T) {
	data := "diff --git a/src/x.c b/src/x.c\nIndex: src/y.c\n--- a/src/x.c\t2026-01-01\n+++ b/src/x.c\n--- /dev/null\n+++ b/new.txt\n*** old/ctx.c\n"
	require.Equal(t, []string{"src/x.c", "y.c", "new.txt", "ctx.c"}, targets([]byte(data), 1))
	require.Equal(t, []string{"a/src/x.c", "b/src/x.c", "src/y.c", "b/new.txt", "old/ctx.c"}, targets([]byte(data), 0))
}

func TestUnreadableArchiveLeavesPatchesUnchecked(t *testing.T) {
	file := filepath.Join(t.TempDir(), "source.tar.gz")
	require.NoError(t, os.WriteFile(file, []byte("archive bytes for /1.2.4/source.tar.gz"), 0600))
	results, err := Check(t.Context(), Request{Archives: []string{file}, Worksrcdir: "project-2.0", PreArgs: []string{"-p0"}, Patches: []Patch{{Name: "good.diff", Data: []byte(good)}}})
	require.NoError(t, err)
	require.False(t, results[0].Checked)
	require.Contains(t, results[0].Detail, "source archive not readable")
	require.Empty(t, Rejected(results))
}
