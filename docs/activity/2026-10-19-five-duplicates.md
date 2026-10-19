# Five duplicates below the engine, each to one composer

The roadmap's v0.5.0 part 1 item 4, "One owner for each rule": the
architecture review of 5897a82's R3-7, R3-18, R3-19, R3-29 with the
code-organization review's finding 14, and R3-26.

## R3-7: the "older than this Mac's MacPorts" sentence

A check's plan note (`macPortsNotes`), setup's status line, and an Xcode
image's setup each composed it, with three grouping loops.

- `engine.OlderMacPorts(behind, reads)` groups `MacPortsBehind` by the two
  versions and says each group once; `engine.RemakeWords(release, xcode)`
  is the setup that makes an image again.
- The plan note now reads "tart macOS 12 (Monterey) arm64 and ... have
  MacPorts 2.12.6, older than this Mac's 2.12.7, which reads their ports:
  dockhand setup tart monterey --rebuild; ...", where it said "build with"
  and "; to make them again:". The Xcode setup's line reads "Its base image
  has MacPorts ..., older than this Mac's ...: dockhand setup tart tahoe
  --rebuild, without --xcode, makes it again."
- Call sites switched: `macPortsNotes` and `remakeWords` (engine),
  `tartLine` and setup tart's base-image line (command/providers.go).
- Grep for the old shape: "older than this Mac's" is written in
  `OlderMacPorts` only.

## R3-18: the Tart 2.39.0 refusal, in tart

`tart.TooOld(version)` is the one refusal, beside `HandlesASIF`; setup's
`ASIFReady` and the provider's start of a check return it. Setup's
wording ("can't list its VMs while one with an ASIF disk runs") gave way
to the provider's. Test: `TestHandlesASIFFromTheVersionTartPrints`.

## R3-19: the liveness window, in coord

- `coord.DefaultHungAfter` is the two minutes; the engine opens the store
  with it as `LiveWithin`, and the store's own two minutes are gone. With
  no window, the store counts every session not ended.
- The store's `LiveUnderMigrationError` says what it found; the engine's
  `LiveUnderMigrationError` wraps it with the way on ("wait for them to
  finish, or stop them, and run this again"), so the words are the
  engine's. The person sees the same sentence.
- Tests: the window in `TestAMigrationWaitsForTheProcessesOnTheOlderSchema`,
  and `TestAMigrationRefusedSaysTheWayOn`.

## R3-29, with finding 14: one release name

- `macos.ReleaseName(platform)` names a release, "macOS 26", or "Darwin 30"
  where macos doesn't know it; planning's `releaseName` and the engine's
  `releaseWords` are gone, and `macos.Describe` names an unknown release
  with it, so `describePlace` no longer rewrites "darwin " to "Darwin ".
- Planning still imports macos for it; R3-15 passes the names in.
- Test: `ReleaseName` and the unknown release in macos's release tests.

## R3-26: evaluator.tcl's `<build>` substitution

`::dockhand::placeholders worker ?sources?` is the one string map; the
code fingerprint takes it with the source roots, the read options without,
as each did.
