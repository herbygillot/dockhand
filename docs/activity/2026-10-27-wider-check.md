# Next names the check a passing host check didn't build

2026-10-27, the docs dig's I47 (roadmap's "Next names the check a passing
host check didn't build"). An attended check builds the `host` profile,
this Mac's release alone, while serve, `bump`, and `ship --yes` build
`minmax`; a pass on the host said nothing of a floor the update raised, or
a variant whose sources it changed, and Next said submit.

- **status reads it** (engine/status.go `wider`, on BranchStatus.Wider and
  branchstate.Branch.Wider): where the current check passed, a person's,
  on the `host` or `local` profile, and the update raised a floor, Next
  is `dockhand check --build minmax`. A floor is a platforms Darwin floor
  or macosx_deployment_target raised (`floorConcerns`, factored out of
  submit's `floorHolds`), or an upstream finding of a newer Go toolchain,
  Python, or C++ standard (`assess.RaisesFloor`). Else, where a variant's
  sources changed, Next is `dockhand check --variants +<variant>`.
- **A variant's sources** (`portfile.SourceVariants`): a variant whose own
  body declares distfiles, checksums, master_sites, or patchfiles on a
  changed line, or whose patchfiles name a file changed under files/.
- The row is `check-wider`, Ready: "passed on this Mac's release alone;
  the update raises the oldest macOS jq builds on, or its toolchain's, so
  a wider check comes first; submit still goes on as it stands". `host`
  stays the attended default, and submit isn't held.
- Tests: TestAPassingHostCheckNamesTheWiderCheck (minmax for a raised
  floor, none once minmax built, the variant for a changed patch, neither
  for neither), TestTheVariantsWhoseSourcesChanged, and the attention test.
