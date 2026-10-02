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
- **A block gated by an option off by default that the Portfile doesn't set** was the dogfood session's proposal, and the person's to decide, since it changes what holds; it's the next section.

Tests: `TestACMakeListsChangeSaysWhereElseItChanged`, and `TestALicenseThatOnlyDropsTextSaysWhat` with entr's year and comma.

## What the default build can't reach (the same day)

Asked whether a change under `if()` blocks gated only by options off by default, which the Portfile doesn't set, should hold, the person chose that it hold nothing, with guards: the file must not set the option, and the Portfile must name it in no variant. The dogfood session's run of 91340a56 on the real archives found two places misnamed beside: "outside any if()" for comments, and "under if(FLB_UTF8_ENCODER)" for blocks added after its `endif()`, which a line diff had paired with theirs. Read against GitHub's compare, the first was also fluent-bit's `set(FLB_VERSION_PATCH 3)`, a version set in parts, which the session's reading had missed.

- **A branch the default build can't take is left out** (`project.cmakeLess`): each `if()`'s and `elseif()`'s condition is read as CMake reads it, parentheses, NOT, AND, and OR, with each option off by default false, constants as they say, and anything else, as a test or another variable, unknown. A branch whose condition is false is cut; an `elseif()` after it opens the block, and an `else()` after it is no longer conditional, so what's compared is what the default build reads.
- **An option is off where nothing turns it on** (`project.CMakeOff`): declared only by `option()`, off in each; no command but the `if()`s that test it, `message()`, and a `cmake_dependent_option()` depending on it takes it as an argument, as `set(FLB_X ON)` or fluent-bit's `FLB_OPTION(FLB_X ON)` would; and the Portfile, as the update left it or as the revision has it, doesn't name it, as a word of its own or after `-D` or `-U` (`portfile.Mentions`). Where the Portfile couldn't be read, every option is taken as one it may set. A name built from a variable, or set in a file the CMakeLists.txt `include()`s, isn't seen: the person chose these guards knowing it, and reading included files is the roadmap's.
- **What's said:** "adds option FLB_PROTOBUF_ENCODER, off by default, which gates find_package(Protobuf); otherwise changes only what the default build doesn't reach, under if(FLB_ALL), under if(FLB_AVRO_ENCODER OR FLB_PROTOBUF_ENCODER), under if(FLB_AVRO_ENCODER): neither it nor the Portfile turns FLB_ALL, FLB_AVRO_ENCODER, or FLB_PROTOBUF_ENCODER on; each option builds as its default", which is fluent-bit 5.1.2 to 5.1.3's real CMakeLists.txt, read with its Portfile. One that only changes comments and blank lines says so, and holds nothing either. Where it holds, the places named are those the default build reaches.
- **Comments aren't changes,** nor are blank lines, and a place is named by the outermost `if()` it's under, as a person finds the block in the file. Lines are compared with the conditions they're under, so an `endif()` pairs only with its own block's.
- **A version set in parts is the version** (`project.DeclaresVersionPart`): `set(FLB_VERSION_PATCH 2)` becoming 3 is 5.1.2 becoming 5.1.3, where the number is that part of each version.
- **`assess.Policy` is 12.**

Tests: `TestWhatTheDefaultBuildCantReachHoldsNothing`, fluent-bit's shape with its comments, its version in parts, and its blocks after `if(FLB_UTF8_ENCODER)`; `TestCMakeLessTakesTheBranchesTheDefaultBuildDoes`, `TestCMakeOffIsWhatNothingTurnsOn`, `TestAVersionSetInPartsIsTheVersion`, `TestAPortfileMentionsAWordOfItsOwn`; `TestAnAddedCMakeOptionHoldsNothing` with a variant naming the option, and the Portfile unread; and `TestAnOptionThePortfileNamesHolds`, through the engine's reading of the Portfile.
