# The patch refresh, the fuzz bound, and patch-rejected's hold

2026-10-27, Then's item 8, its rest (the reflections' B, v1's D12 as the
docs dig's I4, I22, and I9).

- **Fuzz told from offset** (patchcheck): the check runs patch with
  `--verbose`, which says each hunk it placed elsewhere, "Hunk #1
  succeeded at 4 with fuzz 1 (offset 3 lines).", in Apple's and GNU's
  words alike (`Hunks`, `Fuzzed`, `HunkWords`). A patch that applies only
  with fuzz says so, and assess's new `patch-fuzz` holds (policy 29):
  fuzz is never taken unattended, and a person's look is said it.
- **The refresh** (patchcheck/refresh.go `Refresh`): a patch that applies
  at an offset is re-emitted from the tree it made of the new source,
  `diff -u` of each file, its comment and each file's header lines kept,
  only where every moved hunk applied verbatim and its before block
  occurs once in the file. A hunk the new source already carries, its
  after block there and its before block not, is left out (I22): Apple's
  patch skips a whole file once one hunk reads as applied, so patchcheck
  reads that itself, and applies the rest. The prediction is checked
  exactly: the refreshed patch applies with no hunk placed elsewhere, and
  makes byte for byte what the old one did. Files a build generates
  (configure, Makefile.in, …) are said. A fuzzed patch, a context diff,
  and a compressed one aren't refreshed.
- **portedit writes it** (`refreshPatches`, after `dropMergedPatches`): the
  refreshed patch beside the Portfile in files/, in the diff for review,
  the Portfile as it was; update says "Refreshed patch moved.diff: hunk 1
  of dir/b.txt moved to where it now applies; …".
- **patch-rejected holds** (I9): a patch that doesn't apply, the refresh
  not mending it, holds (`Hold`), and an unattended ship stops before it
  proposes the check, the branch at its committed edit: "held before its
  check, at its committed edit: <patch>: <hunks>; holds.allow =
  ["patch-rejected"] lets it build". A person's ship and check build as
  before. Where the patch names a file in git's quoted form, its detail
  says Apple's patch doesn't read it.
- Not done: a fuzzed patch's re-emitted form "offered on an attended run";
  portedit doesn't know whether its run is attended, so a fuzzed patch is
  held and said, and not re-emitted.
- Tests: TestFuzzIsToldFromAnOffset, TestAPatchIsRefreshedToApplyAsWritten,
  portedit's TestPreparationChecksDeclaredPatchesAgainstTheNewSource,
  TestAPatchThatDoesntApplyHoldsAnUnattendedShipBeforeItsCheck, and
  assess's patch-fuzz and holding patch-rejected.
