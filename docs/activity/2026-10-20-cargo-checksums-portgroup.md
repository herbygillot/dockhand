# A PortGroup's command carrying the Portfile's checksums

Field testing on 5a727e00: every cargo port's update refused in seconds,
"the checksums are set by the rust 1.0 PortGroup, at
_resources/port1.0/group/rust-1.0.tcl:253, which no Portfile edit
changes" (purl, fakecloud, wasmtime). 2d61c8c3's update refusal
(`portGroupChecksums`, portedit/dependencies.go) took any checksum
declaration a PortGroup's code made as the PortGroup's. rust 1.0's
checksums-append at line 253 is made by the PortGroup, but it carries each
`cargo.crates` row's sha256, which the Portfile writes and an update
regenerates.

- `portfile.WritesChecksums` reports whether the Portfile writes every
  checksum a declaration carries as a word of its own. A size alone isn't
  enough. The refusal stands only where it doesn't, as with crossbinutils
  1.0's table row.
- Replayed in scratch: `update --new --plan` for purl, fakecloud, and
  wasmtime regenerates `cargo.crates` again, and aarch64-elf-binutils at
  2.48 still refuses as the crossbinutils PortGroup's.
