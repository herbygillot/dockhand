package rewrite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/checksums"
	"github.com/herbygillot/dockhand/internal/edit"
)

// A replacement located where the checksums command spells it keeps the
// kind the caller assembled it with. This package does not decide what a
// replacement is — it decides where it lives — so the kind travels
// through untouched, and a distfile rename stays a distfile rename.
func TestEditsCarryTheReplacementsKind(t *testing.T) {
	src, cst := parse(t, `PortSystem 1.0
checksums foo-1.0.tar.gz rmd160 1111 sha256 2222
`)
	edits, unlocated, viaSet := Edits(src, cst, topLevel, "demo", []checksums.Replacement{
		{Kind: edit.Checksum, Old: "1111", New: "3333", Reason: "checksum rmd160"},
		{Kind: edit.DistfileName, Old: "foo-1.0.tar.gz", New: "foo-2.0.tar.gz", Reason: "distfile name"},
	})
	require.Empty(t, unlocated)
	assert.False(t, viaSet)
	require.Len(t, edits, 2)

	byOld := make(map[string]edit.Kind, len(edits))
	for _, e := range edits {
		byOld[e.Old] = e.Kind
	}
	assert.Equal(t, edit.Checksum, byOld["1111"])
	assert.Equal(t, edit.DistfileName, byOld["foo-1.0.tar.gz"])
}

// The set-carrier fallback is a different act, and it says so: a
// checksum placed by the aliasing heuristic becomes ChecksumSet, which
// the permitted set does not admit. viaSet reports the same fact to the
// caller; the kind is what survives into the record.
func TestSetCarrierEditsAreChecksumSet(t *testing.T) {
	src, cst := parse(t, pcreShaped)
	edits, unlocated, viaSet := Edits(src, cst, topLevel, "pcre2", []checksums.Replacement{
		{Kind: edit.Checksum, Old: "1111bbbb", New: "3333bbbb", Reason: "checksum rmd160"},
	})
	require.Empty(t, unlocated)
	assert.True(t, viaSet)
	require.Len(t, edits, 1)
	assert.Equal(t, edit.ChecksumSet, edits[0].Kind)
}

// Every other kind travels through the fallback unchanged. Relabelling a
// distfile rename as a checksum would name it something it is not, and
// nothing is bought by it: the permitted set admits neither.
func TestSetCarrierLeavesOtherKindsAlone(t *testing.T) {
	src, cst := parse(t, `PortSystem 1.0
set dist foo-1.0.tar.gz
checksums ${dist} rmd160 1111
`)
	edits, unlocated, viaSet := Edits(src, cst, topLevel, "demo", []checksums.Replacement{
		{Kind: edit.DistfileName, Old: "foo-1.0.tar.gz", New: "foo-2.0.tar.gz", Reason: "distfile name"},
	})
	require.Empty(t, unlocated)
	assert.True(t, viaSet)
	require.Len(t, edits, 1)
	assert.Equal(t, edit.DistfileName, edits[0].Kind)
}

// Nothing this package emits may be Unclassified, whichever path found
// it — the sweep a future replacement source has to keep passing.
func TestNoEditLeavesUnclassified(t *testing.T) {
	src, cst := parse(t, pcreShaped)
	edits, _, _ := Edits(src, cst, topLevel, "pcre2", []checksums.Replacement{
		{Kind: edit.Checksum, Old: "1111bbbb", New: "3333bbbb", Reason: "checksum rmd160"},
		{Kind: edit.Checksum, Old: "2222bbbb", New: "4444bbbb", Reason: "checksum sha256"},
	})
	require.NotEmpty(t, edits)
	for _, e := range edits {
		assert.NotEqual(t, edit.Unclassified, e.Kind, e.Reason)
	}
}
