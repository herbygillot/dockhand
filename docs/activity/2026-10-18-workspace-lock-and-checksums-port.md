# A workspace member's Cargo.lock, and checksums with no port

Two findings from field testing at 23c5e2aa.

- **A workspace member's lock is the root's.** `bump purl` (0.2.0 to
  0.2.12) refused, "no extracted archive contains the dependency manifest
  at worksrcdir": finance/purl builds in `${worksrcdir}/cli`, and 0.2.12's
  Cargo.lock is at the workspace root, one level up, where Cargo resolves a
  member's lock. `depblock.CargoLock` reads the lock at worksrcdir, or
  else the nearest above it; the Cargo generator and `ConfirmSource` read
  it, the latter holding the member to the directory it was found for.
  A member's own lock still comes first. TestAWorkspaceMembersLockIsTheRoots.
- **checksums takes the branch's port.** `dockhand checksums` with no
  argument, in a branch worktree, failed "accepts 1 arg(s), received 0",
  where every other branch command finds the port. With none named, it's
  the branch's one port: `BranchStatus.OnlyPort` (engine/status.go), what
  the files change, but _resources, and what the branch was started for;
  a branch about none or several asks for the port, as does `--new`.
  TestChecksumsTakesTheBranchsPort.
