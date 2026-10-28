package patchcheck

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
	"github.com/herbygillot/dockhand/internal/testsupport"
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

// staleHunk matches the stale patch's rejection in either patch's words.
const staleHunk = `(?i)1 out of 1 hunks? failed`

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
	// Apple's patch says "hunks failed" and GNU patch "hunk FAILED".
	require.Regexp(t, staleHunk, results[1].Detail)
	require.False(t, results[2].Applies)
	require.Equal(t, "the source has no dir/gone.txt, which it patches", results[2].Detail, "the file named, where patch said none")
	require.True(t, results[3].Applies, "gzip-compressed patches are decompressed")
	require.False(t, results[4].Checked)
	require.Contains(t, results[4].Detail, "xz compression is not modeled")
	require.Len(t, Rejected(results), 2)
	require.Regexp(t, "stale.diff "+staleHunk, Summary(results))
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

// A source directory no archive holds is one a PortGroup's post-extract
// moves the archive's top directory to, as golang's does: the patches
// apply against that directory (field testing, batch 11: lima).
func TestAMovedSourceIsTheArchivesTopDirectory(t *testing.T) {
	archive := sourceArchive(t, map[string]string{"lima-2.2.1/dir/a.txt": "one\ntwo\nthree\n"})
	results, err := Check(t.Context(), Request{Archives: []string{archive}, Worksrcdir: "gopath/src/github.com/lima-vm/lima", PreArgs: []string{"-t", "-N", "-p0"},
		Patches: []Patch{{Name: "good.diff", Data: []byte(good)}, {Name: "stale.diff", Data: []byte(stale)}}})
	require.NoError(t, err)
	require.True(t, results[0].Applies, results[0].Detail)
	require.False(t, results[1].Applies, "a stale patch is still said")
	require.Regexp(t, staleHunk, results[1].Detail)
}

func TestTargets(t *testing.T) {
	data := "diff --git a/src/x.c b/src/x.c\nIndex: src/y.c\n--- a/src/x.c\t2026-01-01\n+++ b/src/x.c\n--- /dev/null\n+++ b/new.txt\n*** old/ctx.c\n"
	require.Equal(t, []string{"src/x.c", "y.c", "new.txt", "ctx.c"}, targets([]byte(data), 1))
	require.Equal(t, []string{"a/src/x.c", "b/src/x.c", "src/y.c", "b/new.txt", "old/ctx.c"}, targets([]byte(data), 0))

	// git's quoted names, for a space, a tab, or a byte past ASCII, are
	// unquoted, where they were skipped (the docs dig's I30).
	quoted := "diff --git \"a/my file.c\" \"b/my file.c\"\n--- \"a/my file.c\"\n+++ \"b/my file.c\"\n" +
		"diff --git \"a/caf\\303\\251.c\" \"b/caf\\303\\251.c\"\n--- \"a/caf\\303\\251.c\"\t2026-01-01\n+++ \"b/caf\\303\\251.c\"\n"
	require.Equal(t, []string{"my file.c", "café.c"}, targets([]byte(quoted), 1))
}

// A compressed patch that decompresses past the 64 MiB checked of one is
// refused, and left unchecked, as other unreadable patches are, rather than
// checked as its first 64 MiB; the others are still checked.
func TestACompressedPatchPastTheBoundIsUncheckedNotCut(t *testing.T) {
	archive := sourceArchive(t, map[string]string{"project-2.0/dir/a.txt": "one\ntwo\nthree\n"})
	var gzipped bytes.Buffer
	gz := gzip.NewWriter(&gzipped)
	_, _ = gz.Write([]byte(good))
	_, _ = gz.Write(bytes.Repeat([]byte("\n"), maxPatchedFile))
	require.NoError(t, gz.Close())
	results, err := Check(t.Context(), Request{Archives: []string{archive}, Worksrcdir: "project-2.0", PreArgs: []string{"-p0"}, Patches: []Patch{
		{Name: "large.diff.gz", Data: gzipped.Bytes()}, {Name: "good.diff", Data: []byte(good)},
	}})
	require.NoError(t, err)
	require.Equal(t, Result{Name: "large.diff.gz", Detail: "decompresses to more than the 64 MiB dockhand checks of a patch"}, results[0])
	require.True(t, results[1].Applies, results[1].Detail)
	require.Contains(t, Summary(results), "large.diff.gz unchecked (decompresses to more than the 64 MiB")
}

// patch's exit status is its verdict, however much it says: a patch that
// applies saying more than the MiB of its output kept applies, and one that
// doesn't is rejected, where the overflow failed the whole check. patch
// here says it, standing in for one that prints a line for each of many
// files.
func TestPatchsExitStatusDecidesPastItsOutputLimit(t *testing.T) {
	real, err := exec.LookPath("patch")
	require.NoError(t, err)
	bin := t.TempDir()
	testsupport.WriteExecutable(t, filepath.Join(bin, "patch"), "#!/bin/sh\n"+
		"if [ \"$1\" = --version ]; then exec "+real+" --version; fi\n"+
		"cat >/dev/null\n"+
		"yes 'checking file dir/a.txt' | head -c 2097152\n"+
		"exit \"$FAKE_PATCH_EXIT\"\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	archive := sourceArchive(t, map[string]string{"project-2.0/dir/a.txt": "one\ntwo\nthree\n"})
	request := Request{Archives: []string{archive}, Worksrcdir: "project-2.0", PreArgs: []string{"-p0"}, Patches: []Patch{{Name: "good.diff", Data: []byte(good)}}}

	t.Setenv("FAKE_PATCH_EXIT", "0")
	results, err := Check(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, Result{Name: "good.diff", Checked: true, Applies: true, Detail: "applies"}, results[0])

	t.Setenv("FAKE_PATCH_EXIT", "1")
	results, err = Check(t.Context(), request)
	require.NoError(t, err)
	require.True(t, results[0].Checked)
	require.False(t, results[0].Applies)
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

// A patch the source already holds, as upstream merging it leaves it,
// applies in reverse, and is said to be merged; one that fails both ways
// isn't (the roadmap's item 7, files preparation deletes).
func TestAPatchTheSourceAlreadyHoldsIsMerged(t *testing.T) {
	archive := sourceArchive(t, map[string]string{"project-2.0/dir/a.txt": "one\nTWO\nthree\n"})
	results, err := Check(t.Context(), Request{Archives: []string{archive}, Worksrcdir: "project-2.0", PreArgs: []string{"-p0"}, Patches: []Patch{
		{Name: "merged.diff", Data: []byte(good)}, {Name: "stale.diff", Data: []byte(stale)},
	}})
	require.NoError(t, err)
	require.Equal(t, Result{Name: "merged.diff", Checked: true, Merged: true, Detail: "is already in the source: it applies in reverse"}, results[0])
	require.False(t, results[1].Merged)
	require.False(t, results[1].Applies)
}

// A GOPATH source directory with extract.rename is the archive's top
// directory, renamed and moved there whole, as golang's moves maddy's
// (field testing at a9cef1f8: maddy's patch-build.sh.diff, written
// ./build.sh, read as finding no file to patch).
func TestARenamedGOPATHSourceIsTheArchivesTopDirectory(t *testing.T) {
	archive := sourceArchive(t, map[string]string{"maddy-0.9.6/build.sh": "one\ntwo\nthree\n"})
	patch := strings.ReplaceAll(good, "dir/a.txt", "./build.sh")
	results, err := Check(t.Context(), Request{Archives: []string{archive}, Worksrcdir: "gopath/src/github.com/foxcpp/maddy", Rename: true, PreArgs: []string{"-t", "-N", "-p0"},
		Patches: []Patch{{Name: "patch-build.sh.diff", Data: []byte(patch)}}})
	require.NoError(t, err)
	require.True(t, results[0].Applies, results[0].Detail)
}

// patch.args follows patch.pre_args on MacPorts' patch command line, so
// its -p1 is the strip level a Git-style patch applies at, as herdr's
// does (field testing at 5eb101a3: applied at -p0, "No file to patch").
func TestPatchArgsStripLevelIsMacPorts(t *testing.T) {
	archive := sourceArchive(t, map[string]string{"herdr-0.9.5/src/update.rs": "one\ntwo\nthree\n"})
	patch := strings.ReplaceAll(strings.ReplaceAll(good, "--- dir/a.txt", "--- a/src/update.rs"), "+++ dir/a.txt", "+++ b/src/update.rs")
	info := macports.PortInfo{Options: map[string]string{"worksrcdir": "herdr-0.9.5", "extract.rename": "1", "patch.pre_args": "-t -N -p0", "patch.args": "-p1"}}
	results, err := Port(t.Context(), info, []string{archive}, []Patch{{Name: "patch-macports-update.diff", Data: []byte(patch)}}, false)
	require.NoError(t, err)
	require.True(t, results[0].Applies, results[0].Detail)
	delete(info.Options, "patch.args")
	results, err = Port(t.Context(), info, []string{archive}, []Patch{{Name: "patch-macports-update.diff", Data: []byte(patch)}}, false)
	require.NoError(t, err)
	require.False(t, results[0].Applies, "at -p0 alone it finds no file")
}

// A patch that applies with a hunk at an offset says so, and one that
// applies only with fuzz says that, where both read as applying: a fuzzed
// hunk can land in the wrong place and still build (v1's D12, the docs
// dig's I4).
func TestFuzzIsToldFromAnOffset(t *testing.T) {
	archive := sourceArchive(t, map[string]string{"project-2.0/dir/a.txt": "x\ny\nz\none\ntwo\nthree\nfour\nfive\n"})
	offset := "--- dir/a.txt\n+++ dir/a.txt\n@@ -1,5 +1,5 @@\n one\n two\n-three\n+THREE\n four\n five\n"
	fuzz := "--- dir/a.txt\n+++ dir/a.txt\n@@ -4,5 +4,5 @@\n ONE\n two\n-three\n+THREE\n four\n FIVE\n"
	results, err := Check(t.Context(), Request{Archives: []string{archive}, Worksrcdir: "project-2.0", PreArgs: []string{"-p0"}, Patches: []Patch{{Name: "offset.diff", Data: []byte(offset)}, {Name: "fuzz.diff", Data: []byte(fuzz)}}})
	require.NoError(t, err)
	require.True(t, results[0].Applies)
	require.False(t, results[0].Fuzzed())
	require.Equal(t, []Hunk{{Number: 1, Line: 4, Offset: 3}}, results[0].Hunks)
	require.Equal(t, "applies, hunk 1 at an offset of 3 lines", results[0].Detail)
	require.True(t, results[1].Applies)
	require.True(t, results[1].Fuzzed())
	require.Equal(t, "applies only with fuzz: hunk 1 with fuzz 1", results[1].Detail)
}

// A patch that applies at an offset is re-emitted to apply as written,
// its comment and header kept, and one with a hunk the source already
// carries is re-emitted without it; the refreshed patch makes what the old
// one made. One that takes fuzz isn't refreshed (the reflections' B, the
// docs dig's I22).
func TestAPatchIsRefreshedToApplyAsWritten(t *testing.T) {
	source := "x\ny\nz\none\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\n"
	archive := sourceArchive(t, map[string]string{"project-2.0/dir/a.txt": source})
	request := func(name, data string) Request {
		return Request{Archives: []string{archive}, Worksrcdir: "project-2.0", PreArgs: []string{"-p0"}, Patches: []Patch{{Name: name, Data: []byte(data)}}}
	}
	offset := "Keep three upper-case.\n\n--- dir/a.txt\n+++ dir/a.txt\n@@ -1,5 +1,5 @@\n one\n two\n-three\n+THREE\n four\n five\n"
	results, err := Check(t.Context(), request("offset.diff", offset))
	require.NoError(t, err)
	require.NotNil(t, results[0].Refresh)
	require.Equal(t, []string{"hunk 1 of dir/a.txt"}, results[0].Refresh.Moved)
	require.Equal(t, "Keep three upper-case.\n\n--- dir/a.txt\n+++ dir/a.txt\n@@ -3,7 +3,7 @@\n z\n one\n two\n-three\n+THREE\n four\n five\n six\n", string(results[0].Refresh.Data))
	again, err := Check(t.Context(), request("offset.diff", string(results[0].Refresh.Data)))
	require.NoError(t, err)
	require.Equal(t, "applies", again[0].Detail, "as written")
	require.Nil(t, again[0].Refresh)

	// The first hunk upstream already carries; the second still applies.
	carried := "--- dir/a.txt\n+++ dir/a.txt\n@@ -1,3 +1,3 @@\n x\n-Y\n+y\n z\n@@ -9,3 +9,3 @@\n six\n-seven\n+SEVEN\n eight\n"
	results, err = Check(t.Context(), request("carried.diff", carried))
	require.NoError(t, err)
	require.False(t, results[0].Applies, "as patch reads it, the file skipped")
	require.NotNil(t, results[0].Refresh)
	require.Equal(t, []string{"hunk 1 of dir/a.txt"}, results[0].Refresh.Dropped)
	require.Contains(t, string(results[0].Refresh.Data), "-seven\n+SEVEN\n")
	require.NotContains(t, string(results[0].Refresh.Data), "-Y\n")
	require.Equal(t, "hunk 1 of dir/a.txt left out, the new source carries it", results[0].Refresh.Words())

	fuzz := "--- dir/a.txt\n+++ dir/a.txt\n@@ -4,5 +4,5 @@\n ONE\n two\n-three\n+THREE\n four\n FIVE\n"
	results, err = Check(t.Context(), request("fuzz.diff", fuzz))
	require.NoError(t, err)
	require.True(t, results[0].Fuzzed())
	require.Nil(t, results[0].Refresh, "fuzz is a person's look")

	// An attended edit's is re-emitted where patch put it, its fuzzed hunk
	// said for the person to look over; it applies as written, and makes
	// what the old one made with fuzz.
	attended := request("fuzz.diff", fuzz)
	attended.Fuzz = true
	results, err = Check(t.Context(), attended)
	require.NoError(t, err)
	require.NotNil(t, results[0].Refresh)
	require.Equal(t, []string{"hunk 1 of dir/a.txt with fuzz 1"}, results[0].Refresh.Fuzzed)
	require.Equal(t, "hunk 1 of dir/a.txt with fuzz 1 written where patch put it: look it over in the diff", results[0].Refresh.Words())
	again, err = Check(t.Context(), request("fuzz.diff", string(results[0].Refresh.Data)))
	require.NoError(t, err)
	require.Equal(t, "applies", again[0].Detail)
}

// Under extract.mkdir, an archive with no top directory is unpacked in the
// source directory, so its patch applies; without it, MacPorts would
// unpack the files beside the source directory, and the patch finds none
// (the abook session, 2026-10-28: patch-database-ctype.diff held as
// rejected, where the build patched database.c).
func TestAnArchiveUnpackedInItsSourceDirectoryIsReadThere(t *testing.T) {
	archive := sourceArchive(t, map[string]string{"dir/a.txt": "one\ntwo\nthree\n", "README": "unrelated\n"})
	info := macports.PortInfo{Options: map[string]string{"worksrcdir": "abook-0.6.2", "extract.mkdir": "yes", "patch.pre_args": "-t -N -p0"}}
	results, err := Port(t.Context(), info, []string{archive}, []Patch{{Name: "good.diff", Data: []byte(good)}}, false)
	require.NoError(t, err)
	require.True(t, results[0].Applies, results[0].Detail)
	delete(info.Options, "extract.mkdir")
	results, err = Port(t.Context(), info, []string{archive}, []Patch{{Name: "good.diff", Data: []byte(good)}}, false)
	require.NoError(t, err)
	require.False(t, results[0].Applies, "unpacked as MacPorts would without it, the source directory holds none of them")
}
