# Roadmap: the command package split placed in v0.4.0, aliases and bump

The coordinator relayed the coalescing of the command package review's
section 3 (the split) and section 7 (the second scan), with the UX thread's
answer on the hidden aliases and `bump`, at the person's "Send, split in
v0.4.0" (2026-10-15 20:52Z) and their word that `bump` stays. The source is
the project's plan/command-package-split-coalesced-2026-10-15.md, checked at
64c3cb47. Applied to docs/roadmap.md:

- v0.4.0 is about 21 batches: part 1 about 8, part 3 about 7.
- Part 1 hygiene: init, restore, providers (with providers setup tart), and
  clean --archived deleted, with their tests moved, docs fixed, acceptance
  rows A2, F7, B9, and A12 re-pointed, `SuggestFor` on setup and undo, and
  the help-reference test widened to docs/*.md; one `clean --archived
  --plan` and one `clean --legacy --plan` on the person's Mac; bridge 1
  removed in the same step; the split's small relocations; PR #4 first;
  `term` cut last.
- 1c: D-C4 gains `github.LoginStatus`, `setup --json` with `SetupReport`,
  `auth` deleted, its rows re-pointed, and README.md:118's known issue
  dropped; the launchd wait moves the agent, `Notify`, and `Open` to `macos`
  and drops tart/host's uncalled `Start`/`Stop`; SQL-O8 takes the
  `refreshPullRequests` fallbacks; the Branches selector asks one question.
- Part 2: "no package split" replaced by the split's order; item 3 gains
  `BaselineComparison` (from 5.5), the baseline auto-run, the tidy rule,
  `startSession`, the plan readings, and a pinned version; item 4's
  `bump.go` becomes ship's single-port front, with the open rule for a
  resumed branch and its default; item 5 gains the observer, `Stopped`,
  `readServe`, and the selector rows.
- Part 3: `compose`, the `setup` verb, and `cmdtest` with X1; then wave 1,
  `view`, the `run` library, and the verbs, `bump` among them.
- v0.5.0: status and check cut as wave 2 right after 5.5, which takes
  `submitNext`.

## Resettled, the same evening

At the person's ask to resettle (21:16Z, relayed with the project's
plan/v0.4.0-resettled-2026-10-15.md, read at c10e2ea4): no order changes,
and five stale lines fixed. v0.3.1 is cut on release/v0.3 from 3892f092
with 147dc4a9 (07fae561 on main), v0.3.1-rc1 on 147dc4a9; Submit's B4
(82d75484), D-C4 (c10e2ea4), the setup.py item (4ba5aae6), and the
distfile and patch items (b495055a) are marked done; `auth` went with D-C4
and stays in setup's `SuggestFor`; part 3's setup verb takes what 7a rows 3
and 4 and 7b row 4 leave, bridge 1 being settled in hygiene; and PR #4
also wires up `interrupted` (356d5693), merges cleanly onto c10e2ea4, and
holds up only hygiene. The fork another account owns stays a warning, at
the person's "Keep the warning" (21:34Z).
