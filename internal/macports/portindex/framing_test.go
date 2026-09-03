package portindex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/macports"
)

// A misframed index is refused by name rather than read. The two ways
// to misframe one are the two ways this package has had it wrong: a
// length counted in bytes, and a length counted in code points. Both
// leave the frame somewhere other than the payload's own newline, and
// the alternative to refusing is reading a neighbouring entry with its
// name eaten and calling it a port.
func TestMisframedIndexIsRefusedByName(t *testing.T) {
	// A payload whose byte length, character count and UTF-16 length
	// all differ: two en-dashes and one astral character.
	payload := `name widget portdir devel/widget description {a – b – c 🥑}`

	cases := []struct {
		name    string
		declare func(body string) int
	}{
		{"a byte count, as a reader of this index once assumed", func(b string) int { return len(b) }},
		{"a code-point count, as a tclsh without UTF-16 lengths would write", func(b string) int { return len([]rune(b)) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			body := payload + "\n"
			var index strings.Builder
			fmt.Fprintf(&index, "widget %d\n%s", tc.declare(body), body)
			// A second entry, so there is something after the frame to
			// be misread. Its own length is correct.
			index.WriteString(entryText("zlib", `name zlib portdir archivers/zlib`))
			require.NoError(t, os.WriteFile(filepath.Join(root, macports.IndexFile), []byte(index.String()), 0o644))

			_, err := Open(root)
			require.ErrorIs(t, err, ErrMalformed)
			assert.Contains(t, err.Error(), `"widget"`, "the refusal names the entry it could not frame")
		})
	}
}

// The declared length covers the payload's trailing newline, and there
// is no separator byte: the next entry's header begins immediately
// where the frame ends. A fixture that writes a separate newline
// produces an index this reader cannot walk, which is the shape the
// synthetic fixture used to have.
func TestFrameEndsWhereTheNextHeaderBegins(t *testing.T) {
	root := t.TempDir()
	first := entryText("aalib", `name aalib portdir graphics/aalib`)
	second := entryText("zlib", `name zlib portdir archivers/zlib`)
	require.NoError(t, os.WriteFile(filepath.Join(root, macports.IndexFile), []byte(first+second), 0o644))

	ix, err := Open(root)
	require.NoError(t, err)
	assert.Equal(t, 2, ix.Len())
	// The accelerator's offsets are byte offsets, so this is the same
	// arithmetic a real quick file encodes.
	e, err := ix.Lookup("zlib")
	require.NoError(t, err)
	assert.Equal(t, "archivers/zlib", e.Portdir)
	assert.Equal(t, int64(len(first)), mustOffset(t, ix, "zlib"))
}

func mustOffset(t *testing.T, ix *Index, key string) int64 {
	t.Helper()
	off, ok := ix.offsets[key]
	require.True(t, ok)
	return off
}

// A final entry written without its newline is not a misframe: nothing
// follows it to be misread.
func TestFinalEntryMayLackItsNewline(t *testing.T) {
	root := t.TempDir()
	payload := `name aalib portdir graphics/aalib`
	require.NoError(t, os.WriteFile(
		filepath.Join(root, macports.IndexFile),
		fmt.Appendf(nil, "aalib %d\n%s", len(payload), payload), 0o644))

	ix, err := Open(root)
	require.NoError(t, err)
	e, err := ix.Lookup("aalib")
	require.NoError(t, err)
	assert.Equal(t, "graphics/aalib", e.Portdir)
}

func TestDependencyName(t *testing.T) {
	cases := []struct{ token, want, why string }{
		{"port:R-pbapply", "R-pbapply", "the common form: 218859 of them"},
		{"lib:libglib.2:glib2", "glib2", "the test field is what to look for, not what provides it"},
		{"bin:7za:p7zip", "p7zip", ""},
		{"path:share/doc/libgcc/README:libgcc", "libgcc", ""},
		{"path:lib/pkgconfig/libuv.pc:libuv", "libuv", "a test field full of dots and slashes"},
		{"port:bin/cmake:cmake", "cmake", "the one token in the tree that breaks the port: shape"},
		{"legacy-bare-name", "legacy-bare-name", "no colon: the token is the port"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, DependencyName(tc.token), "%s %s", tc.token, tc.why)
	}
}

func TestMaintainers(t *testing.T) {
	cases := []struct {
		field string
		keys  []string
		none  bool
		why   string
	}{
		{
			field: `{@patarra gmail.com:patarra} openmaintainer`,
			keys:  []string{"gh:patarra", "mail:patarra@gmail.com"},
			why:   "a nested element is one person under two spellings, plus a keyword",
		},
		{
			field: `{nicos @NicosPavlov} openmaintainer`,
			keys:  []string{"gh:nicospavlov", "mail:nicos@macports.org"},
			why:   "a bare word is a MacPorts address by the project's own convention",
		},
		{
			field: `{ryandesign @ryandesign} {mathiesen.info:macintosh @BjarneDMat} openmaintainer`,
			keys:  []string{"gh:bjarnedmat", "gh:ryandesign", "mail:macintosh@mathiesen.info", "mail:ryandesign@macports.org"},
			why:   "two people, four spellings",
		},
		{
			field: `nomaintainer`,
			none:  true,
			why:   "the absence of a maintainer, not a maintainer named nomaintainer",
		},
		{
			field: `{nomaintainer openmaintainer}`,
			none:  true,
			why:   "the keywords survive nesting",
		},
		{
			field: `gmail.com:huanguan1978:crown.hg`,
			keys:  []string{"mail:huanguan1978:crown.hg@gmail.com"},
			why:   "the obfuscated form is domain-first; what the rest means is the port's business",
		},
	}
	for _, tc := range cases {
		keys, none := Maintainers(tc.field)
		assert.Equal(t, tc.keys, keys, "%s: %s", tc.field, tc.why)
		assert.Equal(t, tc.none, none, "%s: %s", tc.field, tc.why)
	}
}
