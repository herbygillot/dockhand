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
