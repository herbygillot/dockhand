# outdated: unchecked ports by reason, and a release not ready (v0.4.0 part 1, c)

Two of the Status and outdated items, with the command package review's
`OutdatedPort.Class()` and `OutdatedReport.Summary()` they build.

- **`OutdatedPort.Class()`** is what a look found of a port, the first of
  its facts that holds: own-version, moved, unchecked, uncertain,
  unpublished, newer, current. `OutdatedReport.Summary()` counts by it.
  Plain outdated's rendering switches on the class and takes its counts
  from the summary, where it kept five counters of its own; serve's look
  lists `OutdatedNewer` alone. `--json` gives each port's `class`.
- **Unchecked ports are counted by reason** (the rc10 full stage's B7: the
  daily look's 27 were said with one port's reason). `UncheckedReason` is
  "GitHub's rate limit" for every rate-limited port, whenever each lifts,
  and otherwise the problem's first line; `UncheckedWords` names the
  commonest two and counts the rest, "27 of 320 ports couldn't be checked:
  20 for GitHub's rate limit, 4 for …, 3 more (dockhand outdated --all
  says each)". One reason alone reads as before.
- **A release not ready is said** (field testing at 43949b89: browsh 1.8.3
  was listed as an update that update refused, its release's files not
  yet uploaded). Upstream discovery's `askAssets` asks a newer release
  whether it has files, for a port whose catalog is its releases
  (github.tarball_from releases), one `Assets` request per such newer
  port; none is `Result.Unpublished`, carried as `OutdatedPort.Unpublished`.
  outdated lists it, "nothing yet: the release of v1.8.3 has no files yet,
  which update fetches; it's not ready", and counts it apart; update
  --outdated and serve skip it with that reason. A request that fails
  leaves the release as it was, for update to say when it fetches.

Tests: TestUncheckedPortsAreSaidAhead (grouped reasons, classes, the plan's
skip), TestANewerReleaseWithoutItsFilesIsntReady,
TestOutdatedSaysWhenNothingIsNewer and TestOutdatedThenUpdateOutdated (the
row, the count, the JSON class).
