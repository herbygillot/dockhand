package cargo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const justifiedBlock = `cargo.crates \
    aho-corasick                     1.1.3  8e60d3430d3a69478ad0993f19238d2df97c507009a52b3c10addcd7f6bcb916 \
    android-tzdata                   0.1.1  e999941b234f3131b00bc13c22d06e8c5ff726d1b6318ac7eb276997bbb4fef0 \
    zerocopy                        0.7.35  1b9b4fd18abc82b8136838da5d50bae7bdea537c574d8dc1a34ed098d6c166f0`

const maxlenBlock = `cargo.crates \
    aho-corasick     1.1.3   8e60d3430d3a69478ad0993f19238d2df97c507009a52b3c10addcd7f6bcb916 \
    android-tzdata   9.2     e999941b234f3131b00bc13c22d06e8c5ff726d1b6318ac7eb276997bbb4fef0 \
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

// The script-written geometry ruff's block carries: versions
// right-aligned in a narrow field, metadata versions overflowing from
// its left edge, and a long name sliding the whole field right. Assess
// must prove it and Reformat must preserve it for unchanged crates.
const ruffStyleBlock = `cargo.crates \
    adler2                           2.0.1  320119579fcad9c21884f5c4861d16174d0e06250625266f50fe6898340abefa \
    codspeed-criterion-compat-walltime     5.0.1  5c38205d56e2cb4fe04b708de7f9653a3f1b89edbe3a20b28f21e9e525e9e061 \
    toml                          0.9.12+spec-1.1.0  cf92845e79fc2e2def6a5d828f0801e29a2f8acc037becc5ab08595c7d5e9863 \
    toml                          1.1.4+spec-1.1.0  3aace63f4bbcdfc2c965b059de67119c89c4017a70d633be6c104910f67056f5 \
    zerocopy                        0.8.27  0894878a5fa3edfd6da3f88c4805f4c8558e2b996227a3d864f47fe11e38282c`

func TestAssessProvesTheScriptGeometry(t *testing.T) {
	g, ok := Assess(ruffStyleBlock)
	require.True(t, ok, "the round trip must prove this geometry")
	assert.Equal(t, LayoutJustify, g.Layout)
	assert.Equal(t, 8, g.VWidth, "the field is 8 wide (42-34), not the widest version")
}

func TestReformatKeepsUnchangedCratesIdentical(t *testing.T) {
	g, ok := Assess(ruffStyleBlock)
	require.True(t, ok)
	// What the tool would hand back: same triples, its own wide
	// columns. Reformat must restore the proven geometry so unchanged
	// crates render byte-identical to the committed block.
	_, crates, _, pok := parseBlock(ruffStyleBlock)
	require.True(t, pok)
	wide := Format(crates, Geometry{Layout: LayoutJustify, Option: "cargo.crates",
		Indent: 4, ColLeft: 44, VWidth: 20, MinSep: 2, ShaSep: 2})
	assert.NotEqual(t, ruffStyleBlock, string(wide), "the wide layout must differ, or this proves nothing")
	assert.Equal(t, ruffStyleBlock, string(Reformat(wide, g)))
}
