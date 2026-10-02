# 2026-10-01: an added CMake option holds nothing (D12 revisited)

The person revisited D12 after the dogfood run with 58e2d7eb, where fluent-bit 5.1.3's CMakeLists.txt held its update for adding FLB_PROTOBUF_ENCODER, off by default, and `find_package(Protobuf)` under it: "holding because an option got added is too cautious … We should notify the user that we see an added option, and allow it to be whatever the default is, but don't hold on newly discovered CMake options." Asked of the two edges, they chose that what a new option gates follows the option, holding only where it's on by default, and that a default that flips, or an option removed, still holds.

## What changed

- **A CMakeLists.txt that only adds options holds nothing.** Where removing the options it adds (`project.CMakeWithout`), and each `if()` block that gates on one alone that's off by default, leaves the file as it was, but for the version it names, the change is said with a `·` (`build-file-options`): "upstream's CMakeLists.txt adds option FLB_PROTOBUF_ENCODER, off by default, which gates find_package(Protobuf), and changes nothing else the default build reads; each option builds as its default". An option on by default, gating nothing, is said the same way; what it gates is the default build's, and holds, with the summary batch 23 added. Anything else beside an option, a flipped default, or an option removed, holds as D12 has it.
- **An option's default is read as CMake reads a boolean,** on or off whatever its spelling: fluent-bit's `No` read "added, no by default". A variable's reference, as `${BUILD_SHARED_LIBS}`, is said as written.
- **`assess.Policy` is 11,** so fluent-bit-like assessments recorded before are made again.

## Verification

- Tests: `TestAnAddedCMakeOptionIsSaidAndHoldsNothing` (an option off with what it gates, one on gating nothing, one on gating a package, a flipped default, an option beside another change), `TestCMakeWithoutLeavesOutOptionsAndWhatTheyGate`, `TestAnAddedCMakeOptionHoldsNothing`, and `TestACMakeListsOptionsAndPackagesAreRead` for the booleans.
- The full suite with `DOCKHAND_TEST_MACPORTS_TCLSH`, vet, fmt-check, vendor-check, deadcode, and lint.

## A license that only drops text (the same day)

The dogfood run with ce6a206d bumped entr 5.8 to 5.9 cleanly, held on "LICENSE changed", where 5.9's LICENSE had only dropped its "Compatibility Libraries" section, for libraries no longer shipped, rewrapped a paragraph, and moved a year. The dogfood session proposed that a license that only loses text hold nothing. Losing text can narrow a license as surely as gaining it, "MIT or GPL-2" losing "MIT or", or one paragraph of a dual license, so the person chose that it still holds, said plainly. A license file whose words, years and wrapping aside, are the old ones less some now says "upstream's LICENSE only drops text, 21 words from "2) Compatibility Libraries (MacOS and Linux only) …" on", so the look takes seconds (`TestALicenseThatOnlyDropsTextSaysWhat`).


## Where else a held CMakeLists.txt changed (the same day)

The dogfood run with 11d1df2f found fluent-bit 5.1.3 still held, rightly, but said as "changed: option FLB_PROTOBUF_ENCODER added, off by default; …", naming the one change that holds nothing and none that held: lines under `if(FLB_ALL)`, and `if(FLB_AVRO_ENCODER)` re-gated as `if(FLB_AVRO_ENCODER OR FLB_PROTOBUF_ENCODER)`. And entr 5.9 still said only "LICENSE changed", since beside the section it dropped it wrote "Eric Radman, 2012" for "Eric Radman".

- **A held CMakeLists.txt says where else it changes.** Less what `ReadCMake` reads, every option and `find_package` (`project.CMakeRest`), and less what an added option off by default gates, the old file, with its version made the new one's, and the new are compared line by line; each changed line is placed by the innermost `if()` it's under, the lines opening and closing a block counting as the block's, so a re-gated block is named by its condition on each side: "…; besides, lines change under if(FLB_ALL), under if(FLB_AVRO_ENCODER OR FLB_PROTOBUF_ENCODER), under if(FLB_AVRO_ENCODER)". Past five places, the rest are counted. A file past four million line pairs is placed by what lies between its common start and end. It's a pointer for the look, and changes nothing that holds.
- **A license's years and its words' ending punctuation aren't terms.** "Eric Radman, 2012" for "Eric Radman" now leaves entr 5.9's LICENSE as only dropping text, said as before.
- **Not taken: a block gated by an option off by default that the Portfile doesn't set.** The dogfood session proposed that a change under such a block holds nothing, as what an added option off by default gates doesn't. It changes what holds, so it's the person's to decide.

Tests: `TestACMakeListsChangeSaysWhereElseItChanged`, and `TestALicenseThatOnlyDropsTextSaysWhat` with entr's year and comma.
