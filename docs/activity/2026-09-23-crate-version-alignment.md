# 2026-09-23: crate versions keep the column their Portfile uses

xan's bump to 0.61.0 (macports/macports-ports#34844) regenerated its
`cargo.crates` block, and 39 of the 386 rows came out misaligned: the
version ended past column 42, at a different column per row, one space
from its checksum, where every maintained row ends on 42 with two.

The layout reproduces a block's columns from its own rows. A crate's
version is right-aligned, and `render` placed a changed row's version so
it never starts before `floor`, the leftmost version start in the block,
and keeps the field width `right - floor` when the name runs past it.
That rule is cargo2port's layout, and the Codex rows its test copies
show it: a long version starts at the field and runs on past the column,
pushing the checksum along.

xan's Portfile does it the other way. Its long version,
`wasi 0.11.1+wasi-snapshot-preview1`, ends on column 42 like every other
row and extends to the left. Its start, column 13, became the floor, and
29 columns became the field's width, so every changed row whose name
ended past column 13 was pushed right by the difference, and its hash
fell back to the block's smallest gap, one space.

A right-aligned field is now bounded only where the block shows its
start: a token that runs past the column's right edge without the token
before it having pushed it there. Codex's `wasi` row shows it, and its
layout is unchanged. xan's `encoding-index-*` rows run past too, but
only because their names pushed them, so they show nothing; with no
field start, a version ends on the column and never comes closer to the
name than the block's smallest gap.

Re-laying the PR's 386 rows on the pre-bump block now puts 384 on
column 42 with two spaces. The other two are the unchanged
`encoding-index-*` rows, kept byte for byte as the maintainer wrote
them. A test copies xan's rows beside the Codex ones and fails on the
old code with the PR's own symptom.

The pull request itself still carries the misaligned rows; preparing
the update onto it again with this build re-lays the block.
