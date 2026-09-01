package cargo2port

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const justifiedBlock = `cargo.crates \
    aho-corasick                     1.1.3  8e60d3430d3a69478ad0993f19238d2df97c507009a52b3c10addcd7f6bcb916 \
    android-tzdata                   0.1.1  e999941b234f3131b00bc13c22d06e8c5ff726d1b6318ac7eb276997bbb4fef0 \
    zerocopy                        0.7.35  1b9b4fd18abc82b8136838da5d50bae7bdea537c574d8dc1a34ed098d6c166f0`

const maxlenBlock = `cargo.crates \
    aho-corasick     1.1.3   8e60d3430d3a69478ad0993f19238d2df97c507009a52b3c10addcd7f6bcb916 \
    android-tzdata   0.1.1   e999941b234f3131b00bc13c22d06e8c5ff726d1b6318ac7eb276997bbb4fef0 \
    zerocopy         0.7.35  1b9b4fd18abc82b8136838da5d50bae7bdea537c574d8dc1a34ed098d6c166f0`

const multilineBlock = `cargo.crates \
    aho-corasick 1.1.3 8e60d3430d3a69478ad0993f19238d2df97c507009a52b3c10addcd7f6bcb916 \
    android-tzdata 0.1.1 e999941b234f3131b00bc13c22d06e8c5ff726d1b6318ac7eb276997bbb4fef0 \
    zerocopy 0.7.35 1b9b4fd18abc82b8136838da5d50bae7bdea537c574d8dc1a34ed098d6c166f0`

func TestAlignmentReadsEachLayout(t *testing.T) {
	assert.Equal(t, LayoutJustify, Alignment(justifiedBlock), "right-aligned version edge")
	assert.Equal(t, LayoutMaxlen, Alignment(maxlenBlock), "left-aligned version edge")
	assert.Equal(t, LayoutMultiline, Alignment(multilineBlock), "single-space separators")
}

func TestAlignmentDefaultsWhereGeometryCannotSpeak(t *testing.T) {
	// One crate has no cross-line geometry.
	assert.Equal(t, LayoutJustify, Alignment("cargo.crates \\\n    adler2  2.0.0  09035c21d2ccc"))
	// Uniform field widths make justify and maxlen the same bytes; the
	// tree's convention wins, harmlessly.
	uniform := "cargo.crates \\\n" +
		"    aaa  1.0.0  09035c21d2ccc \\\n" +
		"    bbb  2.0.0  8e60d3430d3a6"
	assert.Equal(t, LayoutJustify, Alignment(uniform))
}

func TestAlignmentRefusesWhatItCannotReproduce(t *testing.T) {
	assert.Equal(t, LayoutRagged, Alignment("cargo.crates \\\n\tadler2\t2.0.0\t09035c21d2ccc"), "tabs")
	ragged := "cargo.crates \\\n" +
		"    aho-corasick        1.1.3  8e60d3430d3a6 \\\n" +
		"    zz 0.7.35      1b9b4fd18abc8"
	assert.Equal(t, LayoutRagged, Alignment(ragged), "no invariant edge")
	assert.Equal(t, LayoutRagged, Alignment("cargo.crates \\\n    not a crate line at all here"), "wrong field count")
}

func TestAlignFlagFallsBackToTheConvention(t *testing.T) {
	assert.Equal(t, "--align=justify", LayoutJustify.alignFlag())
	assert.Equal(t, "--align=maxlen", LayoutMaxlen.alignFlag())
	assert.Equal(t, "--align=multiline", LayoutMultiline.alignFlag())
	assert.Equal(t, "--align=justify", LayoutRagged.alignFlag())
}

func TestAlignmentExcusesColumnOverflow(t *testing.T) {
	// Build-metadata versions cannot fit the column; the tool's own
	// justify output prints them from the column's left edge and lets
	// them overrun. That is the layout working, not the layout broken.
	block := `cargo.crates \
    aho-corasick                     1.1.3  8e60d3430d3a69478ad0993f19238d2df97c507009a52b3c10addcd7f6bcb916 \
    android-tzdata                   0.1.1  e999941b234f3131b00bc13c22d06e8c5ff726d1b6318ac7eb276997bbb4fef0 \
    zerocopy                        0.7.35  1b9b4fd18abc82b8136838da5d50bae7bdea537c574d8dc1a34ed098d6c166f0 \
    wasip2                       1.0.3+wasi-0.2.9  20064672db26d7cdc89c7798c48a0fdfac82132ab2e6a4b46e934bc1c3fca9de`
	assert.Equal(t, LayoutJustify, Alignment(block))
}
