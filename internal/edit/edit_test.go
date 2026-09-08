package edit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The zero value is Unclassified, and that is the whole of the safety
// argument: a producer that forgets to stamp writes a kind no permitted
// set contains, so the omission NARROWS what a machine may do instead of
// widening it.
func TestUnclassifiedIsTheZeroValue(t *testing.T) {
	var k Kind
	assert.Equal(t, Unclassified, k)
	assert.Equal(t, Unclassified, Edit{}.Kind)
}

// The constants' numeric values are pinned because they are DURABLE: a
// change's regions are recorded as kinds, so reordering the block would
// silently re-read every stored region as a different one — a Checksum
// becoming a ChecksumSet, which is exactly the distinction the permitted
// set turns on. Appending a kind is free; inserting one is not, and this
// is where that is noticed.
func TestKindValuesAreDurable(t *testing.T) {
	assert.Equal(t, Unclassified, Kind(0))
	assert.Equal(t, Version, Kind(1))
	assert.Equal(t, RevisionReset, Kind(2))
	assert.Equal(t, EpochBump, Kind(3))
	assert.Equal(t, RevisionBump, Kind(4))
	assert.Equal(t, Checksum, Kind(5))
	assert.Equal(t, ChecksumSet, Kind(6))
	assert.Equal(t, VendoredBlock, Kind(7))
	assert.Equal(t, VendoredNew, Kind(8))
	assert.Equal(t, DistfileName, Kind(9))
	assert.Equal(t, ToolchainMin, Kind(10))
	assert.Equal(t, Rider, Kind(11))
}
