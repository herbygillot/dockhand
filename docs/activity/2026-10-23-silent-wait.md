# 2026-10-23: A wait for a slot is said at every level (rc1's D-R2)

Acceptance of v0.5.0-rc1 (quick stage, D-R2): with both of the Mac's VM
slots taken, `check --fresh` printed nothing for 180 seconds, until the row
cut it. v0.4.0-rc1 said "waiting for the Mac's VMs: 2 are running, and
macOS runs two at most" in the same place.

## Cause

Part 5's levels (6bf84474..f669d452) made a check's progress the redrawn
status line's alone at the default level (`checkrun.quiet`). Without a
terminal there's no status line, so nothing showed. The waits were plain
progress events: Tart's wait for a VM (`tart.Provider.slot`) and the
engine's wait for a provider's capacity (`engine.awaitSlot`).

## Fix

- **A kind of its own.** `model.EventWaiting` is a run waiting on
  something outside it. `buildenv.Build.Waiting` reports it beside
  `Progress`; Tart's VM wait uses it, and so does the engine's capacity
  wait.
- **Said at every level.** checkrun's journal prints a wait as a line
  where there's no status line, and shows it on the status line where
  there is one.
- **Where progress is read, a wait is too.** The queue's and logs' latest
  progress (`LatestRunEvent`, now taking several kinds) and status
  --watch's redraw count a wait.

## Tests

- TestAForegroundCheckKeepsToTheProvidersCapacity runs its checks at the
  default level, not `-v`. It passes with the fix and fails without it.

## Not changed: crystal's link scan

Field testing found no Links row on crystal 1.22.1's check (check-562), and
suspected `engine/links.go` skipping a cell whose advisory tests failed. It
doesn't: an advisory test failure leaves the outcome passed
(`model.TestPolicy.Judge`), and both results hold their links. crystal links
llvm-23, pcre2, boehmgc, libiconv, and libffi, and declares all five in
depends_lib, so no notice is right. That a clean scan says nothing at all
goes to prioritization.

## Then: a clean link scan says it ran

Prioritization placed it in Next, as a fix for main after rc3 (roadmap
de2f447c).

- `evidence.UndeclaredLinks` now also returns a `Clean` finding, counting
  `Ports`, for a target whose builds' links were read and each declared.
  The ports are counted once across environments.
- `view.LinkWords` says it as "crystal links 5 ports, each declared in
  depends_lib", or "jq links no port's libraries" where no port is linked.
  `view.LinkMark` leaves it unmarked, as an unread one is.
- The check's report and status's Links row both print it.
- A failed build is still never read, so it says nothing.
- Tests: TestACleanLinkScanIsSaid has crystal's five links, with failed
  advisory tests, on two releases. TestWriteLinks and the engine's links
  test cover the words.

## The harness: rows that wait on a check's progress run it with -v

Prime-time's rc3 quick stage found that D-I2 never reached its build to
kill. At the default level, off a terminal, a check prints its header, its
results, and its outcome; attempts and provider steps ("building in <vm>")
are -v's, as part 5 intends. So the rows that wait on those lines now run
their background check with `-v`: D-I2, D-S6, D-S9, and D-I5's sleeping
check.

D-R2 stays at the default level on purpose. It tests 74426269, a wait
said at every level.
