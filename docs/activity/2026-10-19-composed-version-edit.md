# A version edit of several literals: openjdk's version and build

Part of the roadmap's v0.5.0 part 3, step 2. openjdk21's tag is
`jdk-${openjdk_version}+${build}`, from `set openjdk_version
${feature}.0.12.1` and `set build 1`. A new release, jdk-21.0.13+5, moves
both literals, and the version probe offered one-literal edits only. So
the update found none and refused ("no version input can select ...",
inferred in the composed-versions note of 2026-10-02).

- The probe keeps a literal that moves the source version but not the
  port's version, as the build number moves the tag alone; it used to drop
  every literal that left the version as it was. A one-literal edit of such
  a carrier still has to change the evaluated version, so none is taken
  alone.
- `versionInputs.composed` builds an edit of two or three plain
  substitution carriers together, when no one literal selects the source.
  Each carrier sits at its own place in the source version as it is. The
  text between them stays, and the new source version is split at that
  text, only where it splits one way (`splitOnce`). The edit is evaluated
  like any other, and exactly one has to select the source.
- `macports.ReleaseInput.Also` records the other literals that moved, for
  a shared release's scope.
- Test: "two literals together" in `TestForwardVersionProbing`, shaped as
  openjdk21's Portfile.

Replayed with `update <port> --new --plan` in a scratch tree, database,
and worktrees, before and after, on layer 1's set (terraform, py-lmdb,
qemu, perl5, kubectl's stub): each plan is the same. openjdk21 to
jdk-21.0.13+8, which refused with "no version input can select
jdk-21.0.13+8", now plans `set openjdk_version ${feature}.0.13` and `set
build 8`, with the archive's checksums, and its two patches apply.
