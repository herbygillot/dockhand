# 2026-09-30: batch 17, what the helper-ownership review reproduced

The [helper-ownership review](../reviews/2026-09-30-helper-ownership.md) read `fe03fa1a`, and its seven probes still failed at `85df763e`. Each is now a regression test, and each of its findings is fixed where its fact lives, with the three smaller rows of its table beside them.

## A requirement's condition (finding 1)

The Python reader stopped at a requirement's `;`, so its PEP 508 marker was dropped:
- A Windows-only pin moving past MacPorts' version held an update serve would otherwise submit.
- A requirement newly applying to macOS changed nothing.
- A package declared twice under two conditions kept only the second.

Now:
- `sourcecompare.Evaluate` reads a marker as a MacPorts build on macOS sees it (`MacOS`): Darwin, POSIX, CPython, and the Python version the port uses where it's known. The answer has three values, yes, no, or unknown, through `and`, `or`, and parentheses. A version is compared as PEP 440 orders it; a machine, an extra, or a Python version not known is unknown, never a guess. A marker that doesn't parse is an error.
- A requirement keeps every declaration of its package, each with its specifier and marker (`Change.Requirements`). A requirement added only for another platform is said and holds nothing. One that applied only elsewhere and now may apply to macOS is as good as added, and holds: "moves requests from >=2; sys_platform == 'win32' to >=2; sys_platform == 'darwin', which now may apply to macOS".
- `pythonPins` skips a declaration whose marker says it applies only elsewhere. It judges that with the Python version of the port providing the package, `macports.PythonVersion` (`py313-requests` is 3.13). One that can't be told is checked as one that applies, as D4 has it.
- A Cargo dependency with both a version and a Git source is read with both, so its revision moving under the same version is a change, counted and holding nothing, as D9 has Cargo's.

## An HTTPS answer that is one (finding 2)

The probe followed a redirect from HTTPS back to plain HTTP and counted it, while `fetch.Open` refuses that downgrade. So `create` could have written an `https://` homepage that answers over HTTP.
- `fetch.Ask` asks a URL for its head, or its first byte, following redirects by the same rule as `Open` (`redirecting`), and says the final URL.
- Its success is its own: any status below 400, not `Open`'s 200.
- The engine's probe calls it, and the engine's boundary test names `fetch`.

## A Portfile's script bodies told from its data (finding 3)

`ArchiveVariants` walked every braced word, so a variant written inside a string, or a `checksums` inside a variant's `set`, was taken for a variant that fetches archives. It now walks with `portfile`'s `commands`, which descends only into the bodies MacPorts runs (`bodies`). A variant a platform block or a condition runs is still found. The version-candidate walk now uses `bodies` too, rather than a copy of its rules, and `BumpRevision` finds a subport's block with `subportBody`, as inspection does.

## The variant reader's failures (finding 4)

`PortInfo.Variants` read `variants` and `vinfo` without their evaluation errors, and dropped a malformed `requires` or `conflicts`. Since item 8, a port whose variants couldn't be read would have planned as declaring none. `macports` now reads options through one checked reading: `option`, `optionList`, `optionDict`, `readList`. Each is a value, an option not set, or an evaluation failure, which is an error, and list and dictionary parse errors are kept. `Variants`, `Bool`, `PortGroups`, and build eligibility share it.

## A category create can write (the table)

`create --category _resources` passed create's own check, which refused only a slash or a space, and would have written the port there. `macports.ValidCategory` is one directory in a port name's characters, and not one the tree keeps for itself, as `IsCategory` has it. `create` checks it before anything is written.

## Which results a baseline rebuilds (the table)

`rebuildWhere` and `BaselineWorthy` each spelled out the install, test, and advisory-failure predicate. `failedAt` is the one rule, a failure while building or before, and both read it.

## The port as an edit found it and leaves it (the table)

`describe` and `preparedPort` read the editor's fidelity history and its unchanged port, while the comparison read `Prepared`. `portedit.Result.PortBefore` and `PortAfter` say the port either way, and every reader uses them, so none needs to know which the result holds.

## Tests

- `TestAMarkerIsReadForMacOS`, `TestARequirementsConditionIsPartOfIt`, `TestAPinForAnotherPlatformHoldsNothing`, and `TestAPythonPortsVersion` cover finding 1.
- `TestAskingAURLFollowsRedirectsAsFetchingDoes` and `TestTheHTTPSProbeRejectsADowngrade` cover finding 2.
- `TestArchiveVariantsReadNoData` covers finding 3.
- `TestVariantsThatCouldntBeReadAreAnError` covers finding 4.
- `TestACategoryANewPortCanGoIn` covers the category, with `create --category _resources` in `TestCreateWritesANewPortFromItsProject`.
- `TestAResultSaysThePortBeforeAndAfter` covers the prepared port.

The review's seven probes are among these, as regression tests. Mutations of the marker logic and the new holds each fail a test.
