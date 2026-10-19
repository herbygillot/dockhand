# One owner for each rule, closed

The roadmap's v0.5.0 part 1 item 4, from the architecture review of
5897a82, closes as Maintenance's relocations say: each commit's note lists
the call sites it switched, and a grep for each old rule's shape, run on
main after the last of them, finds none left.

## The commits

| Commit | Findings | Note |
| --- | --- | --- |
| 2ec02f27 | R3-2, R3-27 (first, for an rc6 that wasn't made) | [one active check](2026-10-19-one-active-check.md) |
| c589a9a3 | R3-1, R3-11 | [holds once](2026-10-19-ship-holds-once.md) |
| 43aa405b | R3-3, R3-25 | [ship selects](2026-10-19-ship-selects.md) |
| 301ba8d7 | R3-35, R3-8 | [tests and the asked check](2026-10-19-tests-and-asked-check.md) |
| 0d4cca4d | R3-9's engine half, R3-36, R3-13 | [who answered, one aside name](2026-10-19-who-answered-and-aside.md) |
| d8616f1e | R3-12, R3-22, R3-37, R3-39's second half | [command owners](2026-10-19-command-owners.md) |
| f3e736c6 | R3-7, R3-18, R3-19, R3-29 with finding 14, R3-26 | [five duplicates](2026-10-19-five-duplicates.md) |
| the next | R3-15, R3-16, R3-28, R3-10's signature, R3-21's second half | [planning, profiles, exports](2026-10-19-planning-profiles-exports.md) |
| this one | R3-33 | docs/architecture.md, below |

## The greps

Each old shape, in non-test Go under internal/, after the last commit:

| Old shape | Left |
| --- | --- |
| `BaselineOf != "" \|\| ... CancelRequested != nil` (R3-2) | none; `CancelRequested != nil` remains where a run's own cancel is read (runner.go, records.go) |
| `"queue"` in a `--json` list, `update --outdated --mine` (R3-27) | none |
| `goOn(`, `shipEdit` (R3-1) | none |
| `NoHandBranch` (R3-11) | none |
| `BranchesChanging(` choosing a port's branch (R3-3) | none; outdated's listing and `PlanOutdated` read it to say a port's in a branch already |
| `errStartForVersion`, `whatFor(` (R3-25) | none |
| `place.Explicit()` (R3-8) | none |
| `s.file.Check.Tests` (R3-35) | none |
| `!plan.Unambiguous()` outside tidy.go (R3-9) | none |
| `inspectRoot` (R3-22) | none |
| `releaseWords(`, planning's `releaseName` (R3-29) | none |
| `spanReason` (R3-15) | none |
| `ReleaseRules`, `BuiltinProfiles`, the engine's own rule consts (R3-16) | none |
| `e.Held(` (R3-10) | none |
| "older than this Mac's" composed (R3-7) | in `OlderMacPorts` only; two comments say it |
| `2 * time.Minute` as a liveness window (R3-19) | in coord only; GitHub's rate wait and Tart's listing wait are other windows |

## Left to other items

- R3-9's command half and R3-39's first half, `attentionFor`: part 4.1.
- R3-10's `ApplySubmit` holding itself: part 4.1.
- R3-21's first half, the macos seams: X1 (item 6).
- R3-21's second half for tart/provision's agent plist: Taken when touched,
  when `tart.SetupProtocol` is next raised (af9e840a).

## R3-33: docs/architecture.md

Brought up to date: the journal `status --watch` follows, where it said
`watch`, and `status --watch` among the processes; `coord.Session` and
the one liveness window; `engine/profile.go`'s rules, config's names, and
planning's marked span; `portfile/move.go` and `portedit/correction.go`;
`engine/restore.go`; `ship` (`command/ship.go`, `engine/ship.go`,
`engine/outdated.go`) choosing its branch through `Select`;
`forge/github/reading.go`'s chunks; `engine/cleanup.go` and
`store/sqlite/prune.go`; and `macos/notify.go`, `macos/agent.go`, and
`macos/launchd.go`.
