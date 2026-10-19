# Where a Portfile sets its revision and checksums, found where the text can't say

The roadmap's v0.5.0 part 3, step 2, its revision and checksums part
(subsuming R2). The text-only rules required one literal block: a
revision bump needed one `revision` in the selected scope, and a checksums
refresh one command of the declaration's text. LyX sets both in each
branch of an `if` on the macOS version. A revbump refused it with "nonzero
revision is set outside the supported scope". Its checksums, the same words
in two branches, were "declaration source is ambiguous (2 matches)" in
the modeled contexts, so `checksums` printed the block to paste by hand.

- `portfile.LocateDeclaration`: of commands of one text, the one a frame of
  this Portfile places on its line is the one that ran. Where none does,
  the refusal is `*portfile.AmbiguousDeclaration`, with its matches.
- `distfiles.BindResolving` takes a resolver for such a declaration, and
  `Bind` is `BindResolving` without one. portedit's `locateChecksums`
  resolves it in this Mac's context by probing: each match's first digest
  is changed, and the one whose change moves the target's evaluated
  checksums is the one that ran, where exactly one does.
- `portfile.RevisionCandidates` lists every literal `revision N` and `set
  name N` of the revision's value, in any body. portedit's `bumpRevision`
  tries `BumpRevision` first, and where the text refuses, raises each
  candidate in turn. It takes the one candidate whose raise raises the
  target's evaluated revision, for a revbump and a stealth update's alike.
  The evaluated edit is held to the raise as before.
- Replayed in a scratch tree, before and after: layer 1's set's `update
  --plan`s (terraform, py-lmdb, qemu, perl5, kubectl's stub) are the same.
  `revbump LyX --plan`, which refused, now plans `revision 1 → 2` in the
  branch this Mac evaluates. `checksums LyX --plan`, which printed the
  block to paste, now reads "all 2 distfiles match their checksums;
  nothing to change".
- Tests: `TestOfTwoCommandsOfOneTextTheFramesLineChooses` and
  `TestRevisionCandidatesAreFoundInEveryBody` (portfile).
- Not found by the probe: checksums a PortGroup keeps in a table, as
  crossbinutils does for aarch64-elf-binutils and the other 72 ports of the
  2026-10-02 survey's "no command written in the Portfile makes it". Those
  aren't the Portfile's to edit.
- `TestSharedReleaseRefusesUnprovableChecksumOwners` now meets fidelity's
  refusal, "fixture-b has independently owned source/checksum
  declarations". Each subport's checksums are located by the frame's line,
  where the text's ambiguity refused first. It's still refused before
  anything is fetched.
