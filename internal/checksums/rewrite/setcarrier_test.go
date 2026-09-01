package rewrite

// Field case (macports-ports-46): six Portfiles tree-wide keep their
// hashes in top-level set variables that the checksums statement
// dereferences — devel/pcre's shape. The values evaluate fine; the
// literals live in `set rmd160(pcre2) <hash>` lines no checksums
// command carries.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/checksums"
)

const pcreShaped = `PortSystem 1.0

set rmd160(pcre)  1111aaaa
set sha256(pcre)  2222aaaa
set size(pcre)    100

set rmd160(pcre2) 1111bbbb
set sha256(pcre2) 2222bbbb
set size(pcre2)   100

checksums           rmd160  $rmd160(${subport}) \
                    sha256  $sha256(${subport}) \
                    size    $size(${subport})
`

func TestEditsFallsBackToSetCarriers(t *testing.T) {
	src, cst := parse(t, pcreShaped)
	edits, unlocated, viaSet := Edits(src, cst, topLevel, "pcre2", []checksums.Replacement{
		{Old: "1111bbbb", New: "3333bbbb", Reason: "checksum rmd160"},
		{Old: "2222bbbb", New: "4444bbbb", Reason: "checksum sha256"},
	})
	require.Empty(t, unlocated)
	require.Len(t, edits, 2)
	assert.True(t, viaSet, "the caller owes the sibling proof")
	assert.Equal(t, "3333bbbb", edits[0].New)
	// The edit landed on pcre2's line, not pcre's.
	assert.Equal(t, "1111bbbb", edits[0].Old)
}

// The aliasing hazard, straight from the field report: two subports
// record an identical size. The array key names the context, so the
// keyed carrier wins and the sibling's line is untouched.
func TestEditsSetCarrierKeyBeatsAnAliasedSibling(t *testing.T) {
	src, cst := parse(t, pcreShaped)
	edits, unlocated, _ := Edits(src, cst, topLevel, "pcre2", []checksums.Replacement{
		{Old: "100", New: "200", Reason: "checksum size"},
	})
	require.Empty(t, unlocated)
	require.Len(t, edits, 1)
	// pcre2's size line starts after pcre's; the span proves which was hit.
	sizeLine := `set size(pcre2)   100`
	wantStart := indexOf(t, pcreShaped, sizeLine) + len(sizeLine) - len("100")
	assert.Equal(t, wantStart, edits[0].Start, "the keyed carrier, never the sibling's aliased value")
}

// Substring keys must not alias: context pcre matches size(pcre), and
// never size(pcre2).
func TestEditsSetCarrierKeyIsExactNotSubstring(t *testing.T) {
	src, cst := parse(t, pcreShaped)
	edits, unlocated, _ := Edits(src, cst, topLevel, "pcre", []checksums.Replacement{
		{Old: "1111aaaa", New: "9999aaaa", Reason: "checksum rmd160"},
		{Old: "100", New: "300", Reason: "checksum size"},
	})
	require.Empty(t, unlocated)
	require.Len(t, edits, 2)
	sizeLine := `set size(pcre)    100`
	wantStart := indexOf(t, pcreShaped, sizeLine) + len(sizeLine) - len("100")
	assert.Equal(t, wantStart, edits[1].Start)
}

// No keyed carrier and more than one candidate: unlocated, honestly —
// a guess between identical values corrupts a sibling.
func TestEditsSetCarrierAmbiguityStaysUnlocated(t *testing.T) {
	src, cst := parse(t, `PortSystem 1.0
set hash_a 1111
set hash_b 1111
checksums rmd160 $hash_a
`)
	edits, unlocated, viaSet := Edits(src, cst, topLevel, "demo", []checksums.Replacement{
		{Old: "1111", New: "2222", Reason: "checksum rmd160"},
	})
	assert.Empty(t, edits)
	assert.False(t, viaSet)
	require.Len(t, unlocated, 1)
}

// A single unkeyed carrier is unambiguous and serves.
func TestEditsSetCarrierSingleUnkeyedServes(t *testing.T) {
	src, cst := parse(t, `PortSystem 1.0
set myhash 1111
checksums rmd160 $myhash
`)
	edits, unlocated, viaSet := Edits(src, cst, topLevel, "demo", []checksums.Replacement{
		{Old: "1111", New: "2222", Reason: "checksum rmd160"},
	})
	require.Empty(t, unlocated)
	require.Len(t, edits, 1)
	assert.True(t, viaSet)
	assert.Equal(t, "2222", edits[0].New)
}

// A literal inside the checksums command still wins outright; the set
// fallback never runs for a located replacement, and viaSet stays
// false for the ordinary port.
func TestEditsChecksumsCommandStillWinsOverSets(t *testing.T) {
	src, cst := parse(t, `PortSystem 1.0
set decoy 1111
checksums rmd160 1111
`)
	edits, unlocated, viaSet := Edits(src, cst, topLevel, "demo", []checksums.Replacement{
		{Old: "1111", New: "2222", Reason: "checksum rmd160"},
	})
	require.Empty(t, unlocated)
	require.Len(t, edits, 1)
	assert.False(t, viaSet)
}

func indexOf(t *testing.T, haystack, needle string) int {
	t.Helper()
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	t.Fatalf("fixture drifted: %q not found", needle)
	return -1
}
