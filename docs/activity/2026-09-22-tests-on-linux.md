# 2026-09-22: the suite on a Linux host

The whole suite now passes in a Linux container, as root and as an
unprivileged user; three tests assumed a Mac and one assumed a
configured Git identity.

- **`macports/eval`**: `TestStartupErrorsAreNotSuccessfulHandshakes`
  needs a `tclsh` and skips without one, as the package's other tests
  skip without MacPorts.
- **`tart/provision`**: `TestAgentRegistrationRequiresObservedService`
  runs the guest's agent registration script under `/bin/zsh`, as
  provisioning runs it in the macOS guest, and skips where there is no
  `/bin/zsh`.
- **`macports/patchcheck`**: not skipped. The check supports GNU patch
  on purpose, choosing `--dry-run` for it and `-C` for Apple's, so the
  test keeps running on Linux; it now accepts the stale patch's
  rejection in either spelling, Apple's "1 out of 1 hunks failed" and
  GNU's "1 out of 1 hunk FAILED".
- **`git`**: `TestCheckoutRejectsSparseAndConflictedIndexes` ran its
  conflicting `git merge` without an identity, so on a host with none
  the merge stopped on "Committer identity unknown" before making the
  conflict, and the capture had nothing to refuse. The merge now carries
  the fixture identity the test's other Git commands already pass.

`verify/tart`'s `TestInterruptedOrChangedImageDoesNotPublishDigest`
failed once in a full run under load and passed in eight reruns since;
it is recorded here, not changed.
