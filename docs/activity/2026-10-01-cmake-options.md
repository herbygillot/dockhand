# 2026-10-01: an added CMake option holds nothing (D12 revisited)

The person revisited D12 after the dogfood run with 58e2d7eb, where fluent-bit 5.1.3's CMakeLists.txt held its update for adding FLB_PROTOBUF_ENCODER, off by default, and `find_package(Protobuf)` under it: "holding because an option got added is too cautious … We should notify the user that we see an added option, and allow it to be whatever the default is, but don't hold on newly discovered CMake options." Asked of the two edges, they chose that what a new option gates follows the option, holding only where it's on by default, and that a default that flips, or an option removed, still holds.

## What changed

- **A CMakeLists.txt that only adds options holds nothing.** Where removing the options it adds (`project.CMakeWithout`), and each `if()` block that gates on one alone that's off by default, leaves the file as it was, but for the version it names, the change is said with a `·` (`build-file-options`): "upstream's CMakeLists.txt adds option FLB_PROTOBUF_ENCODER, off by default, which gates find_package(Protobuf), and changes nothing else the default build reads; each option builds as its default". An option on by default, gating nothing, is said the same way; what it gates is the default build's, and holds, with the summary batch 23 added. Anything else beside an option, a flipped default, or an option removed, holds as D12 has it.
- **An option's default is read as CMake reads a boolean,** on or off whatever its spelling: fluent-bit's `No` read "added, no by default". A variable's reference, as `${BUILD_SHARED_LIBS}`, is said as written.
- **`assess.Policy` is 11,** so fluent-bit-like assessments recorded before are made again.

## Verification

- Tests: `TestAnAddedCMakeOptionIsSaidAndHoldsNothing` (an option off with what it gates, one on gating nothing, one on gating a package, a flipped default, an option beside another change), `TestCMakeWithoutLeavesOutOptionsAndWhatTheyGate`, `TestAnAddedCMakeOptionHoldsNothing`, and `TestACMakeListsOptionsAndPackagesAreRead` for the booleans.
- The full suite with `DOCKHAND_TEST_MACPORTS_TCLSH`, vet, fmt-check, vendor-check, deadcode, and lint.
