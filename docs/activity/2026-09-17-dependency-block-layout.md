# Keeping the maintained layout of regenerated dependency blocks

A Codex bump previewed as a 2,672-line diff because `dependency.Apply` re-emitted every `cargo.crates` row with single spaces whenever any value changed, while the maintained block aligns names, versions, and hashes in columns. Reviewers saw the whole block move instead of the crates that did.

## Design

`Plan.Apply` now infers a layout from each block in the original Portfile before writing the regenerated one. For crate blocks it reads every row's token columns and decides, per column, whether the maintainers align it on its left or right edge and where, and what the smallest gap is when a long token pushes the next one along; the Codex block resolves to a left-aligned name, a version right-aligned to end at column 42, and the hash two spaces later. Go blocks follow go2port's shape, with the first module on the command line, modules on one indent, and key/value pairs on their own columns beneath. Rows whose tokens did not change are reused byte for byte, so they never appear in a diff; changed and new rows are rendered on the inferred columns. A block that does not have the expected shape, such as a wrapped row or a hand-written single-line Go module, yields no layout and falls back to the plain rows, and the whole-block preservation for equivalent values is unchanged. The package-level `Apply` used for provisional evaluation keeps the plain rows.

## Validation

- Tests use rows copied byte for byte from the maintained Codex and mdx Portfiles: unchanged crate and Go rows come back identical, a changed version keeps the hash column, a long crate name and a long version keep the observed gap, the block still ends without a continuation, and the regenerated tokens round-trip. Unusual shapes fall back.
- Live: the Codex 0.155.0-alpha.15 preview went from 2,672 diff lines to 26, of which 15 are added and 11 removed rows plus the version, revision, and checksum edits, with every changed crate row aligned to the maintained columns.
