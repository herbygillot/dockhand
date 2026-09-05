package patch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture is one of the real patchfiles in testdata, copied unmodified
// from macports-ports. The tests build the files they are applied to;
// the patches themselves are exactly what a maintainer committed.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

// The before-blocks of the fixtures' hunks, transcribed here rather
// than derived from the parser, so that the line numbers the cases
// expect are counted against text the test can see. Each is the
// context and removed lines of one hunk, in order, without the leading
// marker column.
const (
	// patch-libraw-no-libstdcxx.diff, Makefile.dist, @@ -167,10 +167,10 @@
	librawMakefile = "\t${CXX} -DLIBRAW_NOTHREADS  ${CFLAGS} -o bin/mem_image samples/mem_image_sample.cpp -L./lib -lraw  -lm  ${LDADD}\n" +
		"\n" +
		"bin/dcraw_half: lib/libraw.a object/dcraw_half.o\n" +
		"\t${CC} -DLIBRAW_NOTHREADS  ${CFLAGS} -o bin/dcraw_half object/dcraw_half.o -L./lib -lraw  -lm -lstdc++  ${LDADD}\n" +
		"\n" +
		"bin/half_mt: lib/libraw_r.a object/half_mt.o\n" +
		"\t${CC}   -pthread ${CFLAGS} -o bin/half_mt object/half_mt.o -L./lib -lraw_r  -lm -lstdc++  ${LDADD}\n" +
		"\n" +
		"bin/dcraw_emu: lib/libraw.a samples/dcraw_emu.cpp\n" +
		"\t${CXX} -DLIBRAW_NOTHREADS  ${CFLAGS} -o bin/dcraw_emu samples/dcraw_emu.cpp -L./lib -lraw  -lm  ${LDADD}\n"
	// patch-libraw-no-libstdcxx.diff, libraw.pc.in, @@ -7,6 +7,6 @@
	librawPC = "Description: Raw image decoder library (non-thread-safe)\n" +
		"Requires: @PACKAGE_REQUIRES@\n" +
		"Version: @PACKAGE_VERSION@\n" +
		"Libs: -L${libdir} -lraw -lstdc++@PC_OPENMP@\n" +
		"Libs.private: @PACKAGE_LIBS_PRIVATE@\n" +
		"Cflags: -I${includedir}/libraw -I${includedir}\n"
	// patch-libraw-no-libstdcxx.diff, libraw_r.pc.in, @@ -7,6 +7,6 @@
	librawRPC = "Description: Raw image decoder library (thread-safe)\n" +
		"Requires: @PACKAGE_REQUIRES@\n" +
		"Version: @PACKAGE_VERSION@\n" +
		"Libs: -L${libdir} -lraw_r -lstdc++@PC_OPENMP@\n" +
		"Libs.private: @PACKAGE_LIBS_PRIVATE@\n" +
		"Cflags: -I${includedir}/libraw -I${includedir}\n"
	// no-fink.patch, configure.ac, @@ -142,9 +142,7 @@
	nettleConfigure = "fi\n" +
		"\n" +
		"LSH_RPATH_INIT([`echo $with_lib_path | sed 's/:/ /g'` \\\n" +
		"    `echo $exec_prefix | sed \"s@^NONE@$prefix/lib@g\" | sed \"s@^NONE@$ac_default_prefix/lib@g\"` \\\n" +
		"    /usr/local/lib /sw/local/lib /sw/lib \\\n" +
		"    /usr/gnu/lib /opt/gnu/lib /sw/gnu/lib /usr/freeware/lib /usr/pkg/lib])\n" +
		"\n" +
		"# Checks for programs.\n" +
		"AC_PROG_CC\n"
	// The comment line nettle's maintainer put above the diff.
	nettleProse = "Do not use paths from Fink, /usr/local, or other non-MacPorts locations.\n"
)

// Synthetic patches for the shapes the fixtures do not have.
const (
	// twoHunks touches one file twice, under git's a/ b/ prefixes. The
	// second hunk's new start is 21: the first added one line.
	twoHunks = "--- a/src/thing.c\t2026-01-01 00:00:00.000000000 +0000\n" +
		"+++ b/src/thing.c\t2026-01-02 00:00:00.000000000 +0000\n" +
		"@@ -3,4 +3,5 @@\n" +
		" alpha\n" +
		" beta\n" +
		"+inserted\n" +
		" gamma\n" +
		" delta\n" +
		"@@ -20,3 +21,2 @@ int main(void)\n" +
		" one\n" +
		"-two\n" +
		" three\n"
	twoHunksFirst  = "alpha\nbeta\ngamma\ndelta\n"
	twoHunksSecond = "one\ntwo\nthree\n"

	// noNewline replaces a file's unterminated last line with another.
	noNewline = "--- end.txt.orig\n" +
		"+++ end.txt\n" +
		"@@ -2,2 +2,2 @@\n" +
		" penultimate\n" +
		"-last old\n" +
		"\\ No newline at end of file\n" +
		"+last new\n" +
		"\\ No newline at end of file\n"

	// creation has no before-block: the file does not exist yet.
	creation = "--- /dev/null\n" +
		"+++ newfile.txt\n" +
		"@@ -0,0 +1,2 @@\n" +
		"+hello\n" +
		"+world\n"

	// zeroContext inserts after line 5 with nothing around it.
	zeroContext = "--- t.orig\n" +
		"+++ t\n" +
		"@@ -5,0 +6 @@\n" +
		"+inserted\n"

	// blankContext has an empty context line an editor stripped the
	// space from, which patch(1) reads as context.
	blankContext = "--- t.orig\n" +
		"+++ t\n" +
		"@@ -1,3 +1,3 @@\n" +
		" a\n" +
		"\n" +
		"-b\n" +
		"+c\n"
)

// archive stands in for the distfile: it serves a file by the path it
// is asked for and remembers every ask, so a case can check what path
// stripping produced and that a creation never asked at all.
type archive struct {
	files map[string]string
	asked []string
}

var errNoSuchMember = errors.New("no such member")

func (a *archive) read(path string) ([]byte, error) {
	a.asked = append(a.asked, path)
	body, ok := a.files[path]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errNoSuchMember, path)
	}
	return []byte(body), nil
}

// pad returns n lines that occur in no before-block, so a target can
// put a block at a chosen line: pad(tag, 4) + block starts the block
// on line 5.
func pad(tag string, n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%s %d\n", tag, i)
	}
	return b.String()
}

// swap is one @@ line the case expects rewritten.
type swap struct{ from, to string }

func TestRelocate(t *testing.T) {
	libraw := fixture(t, "patch-libraw-no-libstdcxx.diff")
	nettle := fixture(t, "no-fink.patch")

	tests := []struct {
		name  string
		patch []byte
		strip int
		files map[string]string

		// A relocation that succeeds: the @@ lines that change, and
		// where every hunk landed.
		swaps  []swap
		placed []Placement
		asked  []string

		// Or one that gives up.
		fail *RelocateError
	}{
		{
			name:  "unchanged: every hunk at its own offset",
			patch: libraw,
			files: map[string]string{
				// 166 lines of padding put the block on line 167.
				"Makefile.dist":  pad("m", 166) + librawMakefile + pad("n", 3),
				"libraw.pc.in":   pad("p", 6) + librawPC,
				"libraw_r.pc.in": pad("q", 6) + librawRPC,
			},
			placed: []Placement{
				{File: "Makefile.dist", Hunk: 1, OldStart: 167, NewStart: 167},
				{File: "libraw.pc.in", Hunk: 1, OldStart: 7, NewStart: 7},
				{File: "libraw_r.pc.in", Hunk: 1, OldStart: 7, NewStart: 7},
			},
			asked: []string{"Makefile.dist", "libraw.pc.in", "libraw_r.pc.in"},
		},
		{
			name:  "moved down: four lines were added above the hunk",
			patch: libraw,
			files: map[string]string{
				// 170 lines of padding put the block on line 171.
				"Makefile.dist":  pad("m", 170) + librawMakefile + pad("n", 3),
				"libraw.pc.in":   pad("p", 6) + librawPC,
				"libraw_r.pc.in": pad("q", 6) + librawRPC,
			},
			swaps: []swap{{"@@ -167,10 +167,10 @@", "@@ -171,10 +171,10 @@"}},
			placed: []Placement{
				{File: "Makefile.dist", Hunk: 1, OldStart: 167, NewStart: 171},
				{File: "libraw.pc.in", Hunk: 1, OldStart: 7, NewStart: 7},
				{File: "libraw_r.pc.in", Hunk: 1, OldStart: 7, NewStart: 7},
			},
		},
		{
			name:  "moved up: two lines were removed above the hunk, prose kept",
			patch: nettle,
			files: map[string]string{
				// 139 lines of padding put the block on line 140.
				"configure.ac": pad("c", 139) + nettleConfigure + pad("d", 5),
			},
			swaps:  []swap{{"@@ -142,9 +142,7 @@", "@@ -140,9 +140,7 @@"}},
			placed: []Placement{{File: "configure.ac", Hunk: 1, OldStart: 142, NewStart: 140}},
		},
		{
			name:  "two hunks in one file move by different amounts",
			patch: []byte(twoHunks),
			strip: 1,
			files: map[string]string{
				// Five lines of padding put the first block on line 6,
				// three below where it was. It is four lines long, so
				// twenty more lines of padding after it put the second
				// block on line 30, ten below where it was. The second
				// hunk's new side goes from 21 to 31: the same ten.
				"src/thing.c": pad("a", 5) + twoHunksFirst + pad("b", 20) + twoHunksSecond,
			},
			swaps: []swap{
				{"@@ -3,4 +3,5 @@", "@@ -6,4 +6,5 @@"},
				{"@@ -20,3 +21,2 @@ int main(void)", "@@ -30,3 +31,2 @@ int main(void)"},
			},
			placed: []Placement{
				{File: "src/thing.c", Hunk: 1, OldStart: 3, NewStart: 6},
				{File: "src/thing.c", Hunk: 2, OldStart: 20, NewStart: 30},
			},
			asked: []string{"src/thing.c"},
		},
		{
			name:  "strip=0 keeps the +++ path whole",
			patch: []byte(twoHunks),
			strip: 0,
			files: map[string]string{
				"b/src/thing.c": pad("a", 2) + twoHunksFirst + pad("b", 13) + twoHunksSecond,
			},
			placed: []Placement{
				{File: "b/src/thing.c", Hunk: 1, OldStart: 3, NewStart: 3},
				{File: "b/src/thing.c", Hunk: 2, OldStart: 20, NewStart: 20},
			},
			asked: []string{"b/src/thing.c"},
		},
		{
			name:  "strip beyond the path's depth keeps the last component",
			patch: []byte(twoHunks),
			strip: 5,
			files: map[string]string{
				"thing.c": pad("a", 2) + twoHunksFirst + pad("b", 13) + twoHunksSecond,
			},
			placed: []Placement{
				{File: "thing.c", Hunk: 1, OldStart: 3, NewStart: 3},
				{File: "thing.c", Hunk: 2, OldStart: 20, NewStart: 20},
			},
			asked: []string{"thing.c"},
		},
		{
			name:  "no-newline marker rides along with a moved hunk",
			patch: []byte(noNewline),
			files: map[string]string{
				// Three lines of padding put the block on line 4; the
				// file's last line has no newline, as the marker says.
				"end.txt": pad("e", 3) + "penultimate\nlast old",
			},
			swaps:  []swap{{"@@ -2,2 +2,2 @@", "@@ -4,2 +4,2 @@"}},
			placed: []Placement{{File: "end.txt", Hunk: 1, OldStart: 2, NewStart: 4}},
		},
		{
			name:  "a file creation has nothing to anchor and never reads",
			patch: []byte(creation),
			files: map[string]string{},
			placed: []Placement{
				{File: "newfile.txt", Hunk: 1, OldStart: 0, NewStart: 0},
			},
			asked: []string{},
		},
		{
			name:  "a stripped blank context line matches an empty line",
			patch: []byte(blankContext),
			files: map[string]string{
				"t": pad("t", 2) + "a\n\nb\n",
			},
			swaps:  []swap{{"@@ -1,3 +1,3 @@", "@@ -3,3 +3,3 @@"}},
			placed: []Placement{{File: "t", Hunk: 1, OldStart: 1, NewStart: 3}},
		},

		{
			name:  "not found: the lines the hunk changes have changed",
			patch: nettle,
			files: map[string]string{
				"configure.ac": pad("c", 141) + strings.Replace(nettleConfigure, "AC_PROG_CC", "AC_PROG_CC_C99", 1),
			},
			fail: &RelocateError{File: "configure.ac", Hunk: 1, Reason: NotFound},
		},
		{
			name:  "ambiguous: the before-block occurs twice",
			patch: nettle,
			files: map[string]string{
				"configure.ac": pad("c", 2) + nettleConfigure + pad("d", 2) + nettleConfigure,
			},
			fail: &RelocateError{File: "configure.ac", Hunk: 1, Reason: Ambiguous},
		},
		{
			name:  "unreadable: the third file is not in the distfile",
			patch: libraw,
			files: map[string]string{
				"Makefile.dist": pad("m", 166) + librawMakefile,
				"libraw.pc.in":  pad("p", 6) + librawPC,
			},
			fail: &RelocateError{File: "libraw_r.pc.in", Hunk: 1, Reason: Unreadable},
		},
		{
			name:  "second hunk not found names the second hunk",
			patch: []byte(twoHunks),
			strip: 1,
			files: map[string]string{
				"src/thing.c": pad("a", 2) + twoHunksFirst + pad("b", 20),
			},
			fail: &RelocateError{File: "src/thing.c", Hunk: 2, Reason: NotFound},
		},
		{
			name:  "CRLF target is not the patch's LF block",
			patch: nettle,
			files: map[string]string{
				"configure.ac": strings.ReplaceAll(pad("c", 141)+nettleConfigure, "\n", "\r\n"),
			},
			fail: &RelocateError{File: "configure.ac", Hunk: 1, Reason: NotFound},
		},
		{
			name:  "CRLF patch is not the target's LF lines",
			patch: []byte(strings.ReplaceAll(string(nettle), "\n", "\r\n")),
			files: map[string]string{
				// The header names end at diff's tab, so the '\r' is
				// in the body lines alone, and that is where the
				// give-up comes from.
				"configure.ac": pad("c", 141) + nettleConfigure,
			},
			fail: &RelocateError{File: "configure.ac", Hunk: 1, Reason: NotFound},
		},
		{
			name:  "a zero-context hunk could be anywhere",
			patch: []byte(zeroContext),
			files: map[string]string{"t": pad("t", 10)},
			fail:  &RelocateError{File: "t", Hunk: 1, Reason: Ambiguous},
		},
		{
			name:  "entangled: the hunks have swapped places",
			patch: []byte(twoHunks),
			strip: 1,
			files: map[string]string{
				"src/thing.c": pad("a", 2) + twoHunksSecond + pad("b", 20) + twoHunksFirst,
			},
			fail: &RelocateError{File: "src/thing.c", Hunk: 2, Reason: Entangled},
		},
		{
			name:  "entangled: one file patched by two sections",
			patch: []byte(twoHunksSplit()),
			strip: 1,
			files: map[string]string{
				"src/thing.c": pad("a", 2) + twoHunksFirst + pad("b", 13) + twoHunksSecond,
			},
			fail: &RelocateError{File: "src/thing.c", Hunk: 1, Reason: Entangled},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.patch)
			require.NoError(t, err)
			a := &archive{files: tt.files, asked: []string{}}
			res, err := p.Relocate(a.read, tt.strip)

			if tt.fail != nil {
				var re *RelocateError
				require.ErrorAs(t, err, &re)
				assert.Equal(t, tt.fail.File, re.File)
				assert.Equal(t, tt.fail.Hunk, re.Hunk)
				assert.Equal(t, tt.fail.Reason, re.Reason)
				if tt.fail.Reason == Unreadable {
					require.ErrorIs(t, err, errNoSuchMember, "the reader's error is reachable")
				}
				assert.Empty(t, res.Bytes, "no partial result")
				return
			}
			require.NoError(t, err)

			// Byte-identical to the input except the @@ lines the
			// case names, each of which must be present exactly once.
			want := string(tt.patch)
			for _, s := range tt.swaps {
				require.Equal(t, 1, strings.Count(want, s.from), "swap %q is one line of the input", s.from)
				want = strings.Replace(want, s.from, s.to, 1)
			}
			assert.Equal(t, want, string(res.Bytes))
			assert.Equal(t, tt.placed, res.Hunks)
			moved := len(tt.swaps)
			assert.Equal(t, moved, res.Moved())
			if tt.asked != nil {
				assert.Equal(t, tt.asked, a.asked)
			}

			// The refreshed patch reads back, and applied to the same
			// files it now sits still.
			again, err := Parse(res.Bytes)
			require.NoError(t, err)
			res2, err := again.Relocate((&archive{files: tt.files}).read, tt.strip)
			require.NoError(t, err)
			assert.Zero(t, res2.Moved())
			assert.Equal(t, res.Bytes, res2.Bytes)
		})
	}
}

// twoHunksSplit is twoHunks as two sections on the same file, the way
// concatenated diffs arrive.
func twoHunksSplit() string {
	header := "--- a/src/thing.c\n+++ b/src/thing.c\n"
	i := strings.Index(twoHunks, "@@ -3")
	j := strings.Index(twoHunks, "@@ -20")
	return header + twoHunks[i:j] + header + twoHunks[j:]
}

// The leading prose is the first thing a reader of the committed file
// sees, and the whole-bytes comparison above already covers it; this
// says so by name, on the byte, in case that comparison is ever
// loosened.
func TestRelocateKeepsLeadingProse(t *testing.T) {
	p, err := Parse(fixture(t, "no-fink.patch"))
	require.NoError(t, err)
	require.Len(t, p.Files, 1)
	assert.Equal(t, nettleProse, string(p.Files[0].Preamble))

	a := &archive{files: map[string]string{"configure.ac": pad("c", 139) + nettleConfigure}}
	res, err := p.Relocate(a.read, 0)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(res.Bytes), nettleProse+"--- configure.ac.orig\t"))
}

// Relocate is a function of its receiver: the parsed patch it was
// called on still renders the input afterwards.
func TestRelocateLeavesReceiverAlone(t *testing.T) {
	src := fixture(t, "no-fink.patch")
	p, err := Parse(src)
	require.NoError(t, err)
	a := &archive{files: map[string]string{"configure.ac": pad("c", 139) + nettleConfigure}}
	res, err := p.Relocate(a.read, 0)
	require.NoError(t, err)
	require.Equal(t, 1, res.Moved())
	assert.Equal(t, src, p.Bytes())
	assert.Equal(t, 142, p.Files[0].Hunks[0].OldStart)
}

func TestRelocateErrorWording(t *testing.T) {
	e := &RelocateError{File: "configure.ac", Hunk: 2, Reason: NotFound}
	assert.Equal(t, "patch: configure.ac hunk #2: its before-block occurs nowhere in the file", e.Error())

	e = &RelocateError{File: "x.c", Hunk: 1, Reason: Unreadable, Err: errNoSuchMember}
	assert.Equal(t, "patch: x.c hunk #1: the file could not be read: no such member", e.Error())
	require.ErrorIs(t, e, errNoSuchMember)
}

// Parse then Bytes is the identity over the real patchfiles. This is
// the property everything else rests on: what Relocate writes back is
// the input, save the numbers it changed, only if the parser kept every
// byte in the first place.
func TestParseRoundTrip(t *testing.T) {
	for _, name := range []string{"patch-libraw-no-libstdcxx.diff", "no-fink.patch"} {
		src := fixture(t, name)
		p, err := Parse(src)
		require.NoError(t, err, name)
		assert.Equal(t, src, p.Bytes(), name)
	}
}

func TestParseStructure(t *testing.T) {
	p, err := Parse(fixture(t, "patch-libraw-no-libstdcxx.diff"))
	require.NoError(t, err)
	require.Len(t, p.Files, 3)
	assert.Empty(t, p.Files[0].Preamble)
	assert.Empty(t, p.Trailer)

	f := p.Files[0]
	assert.Equal(t, "Makefile.dist.orig", f.Old)
	assert.Equal(t, "Makefile.dist", f.New)
	assert.Equal(t, "Makefile.dist", f.Target(0))
	require.Len(t, f.Hunks, 1)
	h := f.Hunks[0]
	assert.Equal(t, [4]int{167, 10, 167, 10}, [4]int{h.OldStart, h.OldCount, h.NewStart, h.NewCount})
	assert.Len(t, strings.Split(strings.TrimSuffix(string(h.Body), "\n"), "\n"), 12, "10 old + 2 added")
	assert.Equal(t, "libraw.pc.in", p.Files[1].New)
	assert.Equal(t, "libraw_r.pc.in", p.Files[2].New)

	// git's prefixes strip; a deletion's target is the --- name.
	g, err := Parse([]byte(twoHunks))
	require.NoError(t, err)
	assert.Equal(t, "src/thing.c", g.Files[0].Target(1))
	assert.Equal(t, "b/src/thing.c", g.Files[0].Target(0))
	d, err := Parse([]byte("--- a/gone.c\n+++ /dev/null\n@@ -1 +0,0 @@\n-only line\n"))
	require.NoError(t, err)
	assert.Equal(t, "gone.c", d.Files[0].Target(1))
	assert.Equal(t, 1, d.Files[0].Hunks[0].OldCount, "a bare range is a count of one")
	assert.Equal(t, 0, d.Files[0].Hunks[0].NewCount)
	assert.Equal(t, "--- a/gone.c\n+++ /dev/null\n@@ -1 +0,0 @@\n-only line\n", string(d.Bytes()), "a bare range renders bare")
}

// A hunk body is measured by its counts, never by looking for the next
// header: a removed line whose content begins "-- " renders as "--- ",
// and a scan for headers would end the hunk there.
func TestParseBodyByCount(t *testing.T) {
	src := "--- t.orig\n+++ t\n@@ -1,3 +1,3 @@\n keep\n--- was a dashed line\n+++ now a plussed line\n tail\n"
	p, err := Parse([]byte(src))
	require.NoError(t, err)
	require.Len(t, p.Files, 1)
	require.Len(t, p.Files[0].Hunks, 1)
	assert.Equal(t, " keep\n--- was a dashed line\n+++ now a plussed line\n tail\n", string(p.Files[0].Hunks[0].Body))
	assert.Equal(t, src, string(p.Bytes()))
}

// Prose between sections and after the last hunk is kept where it was.
func TestParseKeepsProseEverywhere(t *testing.T) {
	src := "From: someone\n\n" +
		"diff --git a/x b/x\nindex 1..2 100644\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n" +
		"diff --git a/y b/y\n--- a/y\n+++ b/y\n@@ -1 +1 @@\n-c\n+d\n" +
		"-- \n2.39.0\n"
	p, err := Parse([]byte(src))
	require.NoError(t, err)
	require.Len(t, p.Files, 2)
	assert.Equal(t, "From: someone\n\ndiff --git a/x b/x\nindex 1..2 100644\n", string(p.Files[0].Preamble))
	assert.Equal(t, "diff --git a/y b/y\n", string(p.Files[1].Preamble))
	assert.Equal(t, "-- \n2.39.0\n", string(p.Trailer))
	assert.Equal(t, src, string(p.Bytes()))
}

func TestParseMalformed(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"empty", ""},
		{"prose only", "just some text\n--- not a header\n"},
		{"context format", "*** a.orig\n--- a\n***************\n*** 1,2 ****\n! x\n--- 1,2 ----\n! y\n"},
		{"header without a hunk", "--- a\n+++ b\n"},
		{"header then prose", "--- a\n+++ b\nnothing here\n"},
		{"unreadable @@ line", "--- a\n+++ b\n@@ -x +y @@\n a\n"},
		{"truncated hunk", "--- a\n+++ b\n@@ -1,3 +1,3 @@\n a\n b\n"},
		{"foreign line inside hunk", "--- a\n+++ b\n@@ -1,3 +1,3 @@\n a\nnot a body line\n c\n"},
		{"too many removed lines", "--- a\n+++ b\n@@ -1,1 +1,1 @@\n-a\n-b\n"},
		{"@@ line in the prose", "@@ -1,2 +1,2 @@\n--- a\n+++ b\n@@ -1 +1 @@\n-x\n+y\n"},
		// The header says two lines and the body has three; the third
		// ends the hunk, and the next @@ line stands outside the
		// section. Without the refusal it would ride into the trailer
		// and Relocate would refresh the first hunk alone.
		{"undercounted hunk before another", "--- a\n+++ b\n@@ -1,2 +1,2 @@\n a\n-b\n+B\n c\n@@ -10,2 +10,2 @@\n d\n-e\n+E\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.src))
			require.ErrorIs(t, err, ErrMalformed)
		})
	}
}

func TestReasonStrings(t *testing.T) {
	for _, r := range []Reason{NotFound, Ambiguous, Unreadable, Entangled} {
		assert.NotEqual(t, "unknown reason", r.String(), "%d", r)
	}
}
