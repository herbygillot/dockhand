# Batch a, first half: the Go toolchain ports update

v0.4.0 part 1's first item (the roadmap's 2a), its first entry: the Go
toolchain ports. Field testing at 1cc87c43 found `update --outdated go-1.26
go-1.27` skipped both, "no editable version input established", and their
updates (#35374, #35375) went up by hand.

Three things stood between update and go-1.26's `go_toolchain.setup 1.26.9`,
each fixed where it belongs, gated by the probe rather than a table:

- `portfile.Candidates`: a `<pg>.setup` command `setupVersionIndex` doesn't
  know offers each of its literal words as a candidate. The existing probe
  keeps the one the evaluated version follows, and refuses where two move it.
- `fidelity.version`: a field that moves only by the version written in it,
  everywhere it's written, is the version's (`writesVersion`). go_toolchain's
  long_description, "provides Go 1.26.8", was refused as changing.
- `portfile.RenameChecksumFiles`, called once the version edit is chosen:
  archive names a checksums declaration lists literally move with the
  version. go-1.26 names its three, `go1.26.9.src.tar.gz` and the two darwin
  binaries.
- The observer's platform scan: a checksums declaration naming an archive for
  an architecture (arm64, amd64, x86_64, aarch64) makes the contexts
  architecture-dependent, so both are sampled. go_toolchain chooses the
  binary in its PortGroup, which the Portfile doesn't read, and the
  darwin-amd64 archive had no context on an arm64 Mac.

`update go-1.26 1.26.8 --plan`, `go-1.27 1.27.1`, and `go-1.25 1.25.13`
now plan the version, all three archives' names and checksums, and compare
upstream. Tests: the probe's unknown setup and its two-word ambiguity,
TestVersionAllowsAFieldThatWritesTheVersion,
TestChecksumFileNamesMoveWithTheVersion, and
TestArchitectureNamedArchivesSampleEachArchitecture.

The other half, `checksums` on just about anything, follows.
