# 2026-09-27: the code-organization review, checked and reconciled

The [code-organization review](../reviews/2026-09-27-code-organization-review.md) read the code at `a71fc67f`, and main had moved 23 commits by the time it was checked: parallel environments, build reuse, cleanup, the outdated count, `bump`, and the shared update path among them. So each of its 46 findings was checked again at `3b16b187`.
- **Who checked.** Seven readers, one per group of findings, each reopening every citation and tracing what the later commits changed. Nothing was edited; one race probe ran as a test overlay, outside the tree.
- **Where the rest went.** What holds is folded into the roadmap: one item before item 6 goes on, pieces bound to items 6 and 7, smaller items, and the Reviews section.

## What the check changed

**Worse than the review said, because of later work.**
- **Finding 1 (P1).** The engine's unlocked first-use assembly is reachable within a single command since `c4f16ba4`, not only under serve: `retry` and `wait` drive a run without planning first, and two Tart releases' executions then assemble the port reader at once. The race detector confirmed it with a probe. The review's "`make test-race` would flag it" was wrong: no test reached the path.
- **Finding 1, missed.** Tart's `Provider.vms()` checks and sets its machine without a lock, and it is the first thing both parallel executions do.
- **Finding 32.** `bump` now depends on the same upstream hold as serve. So the guardrail is silently off for every port with `go.vendors` or `cargo.crates` under both.
- **Finding 35.** Automatic cleanup made one stamp per database decide when each repository's merged branches are cleaned. Commands in one checkout can hold off another's cleanup.
- **Finding 36.** Since `d866ff80`, a build that read a port the guest couldn't place in the ports tree failed every later check of its target, unless `--fresh` was given. It is recorded with no directory, and deciding reuse passed that empty path to `git ls-tree`.
- **Finding 37.** `CleanupDue` returns prose that two callers test by prefix.

**Found beyond it.**
- `update --submit` on an existing branch also bypasses the one-check-per-branch rule (finding 2).
- A refused SSH login is retried for four minutes, three times: about twelve minutes per environment (finding 7).
- The stealth revision bump isn't proven for subports (finding 19).
- An error in one environment now cancels a script build beside it, recorded as canceled (finding 42).
- Reuse can carry an unvalidated tests value (finding 28).
- Status sends a branch with an unchecked `--also` extra to submit, which refuses it with a misleading reason (finding 25).
- The `deadcode` tool can't see unused exported methods, so the maintenance rule's check misses them, and CI doesn't run it (finding 18).

**Narrower or wrong.**
- A misspelled `--tests` is refused, late, rather than run as advisory (finding 2).
- `bump` and serve's preparation don't bypass the one-check rule: each starts its own branch (finding 2).
- `update` doesn't prompt on an untracked branch (finding 8).
- The upstream livecheck drift misreports only a word in `outdated` (finding 6, now P3).
- `outdated`'s `Selection` alias is used (finding 20).
- Submit's merge loop is the only merge gate for a person's submit, and stays (finding 26).
- Reading plans in the old form is item 2's delivered promise, so `decodePlan`'s legacy branch stays (finding 41).
- `command` may not import `macos`, and v2's retirement removed that keep-alive switch on purpose, so the plist half of finding 21 is declined.

**Fixed since the review, by later work.**
- The outdated count, as a callback (`80960866`).
- The edit's port is read (`864085c9`).
- Both are recorded as such.

## Fixed at once

Both regressions are this week's, and each is live:
- **Reuse with a port outside the tree.** `reuse.Paths` skips a port with no directory, as recording already did, and such inputs stay incomplete. `TestABuildReadingAPortOutsideTheTreeIsBuiltAgain` failed with the `git ls-tree` error before the fix.
- **First-use assembly.**
  - The five assemblers go through one `assemble`, which builds outside the engine's lock and sets the field once. A single lock around each would deadlock, since four of them call `selectionReader`.
  - Tart's `vms()` takes a lock of its own.
  - The three engine copies of the system GitHub client are one `github()`.
  - `TestEnvironmentsBuildingTogetherShareWhatTheEngineAssembles` fails under `go test -race` with the old `selectionReader` (two data races) and passes with the new.

## Decisions for the person

- **What the update couldn't check, under unattended submission.**
  - The cases:
    - a comparison that couldn't pair the archives (every `go.vendors` and `cargo.crates` port today);
    - a Go toolchain minimum that couldn't be rewritten;
    - patches left unchecked.
  - Today these are a line of output, or nothing, and `bump` and serve submit without them.
  - Recommended: they hold, as a failed search for other pull requests already does.
- **Whether tidy, rebase, and restore move into `history`** (finding 22). Recommended: they stay, and the review's own `Transitions.Make` puts the ref change beside its recognition.
- **Finding 10's branch shape in JSON** needs no decision. The person's rule, revised the same day, is one shape per command wherever it stops, and commands needn't share one ([note](2026-09-27-one-json-shape.md)). The baseline marker is taken with that rule.
