# 2026-09-27: a new roadmap, reconciled with the architecture review

The person asked for the [architecture and data-flow review](../reviews/2026-09-27-architecture-and-data-flow.md) to be synthesized with the roadmap into a new one.

## The review, checked

- **Its probes.** They were applied in a scratch worktree at `52d03e2a`, not in the shared checkout, and all seven failed as the review says. The worktree was removed afterwards.
- **The claims behind them, checked in the code:**
  - The github provider never reads the plan's test policy; only Tart's guest program applies it.
  - The pull request checklist counts only failed tests as not passed, so timed-out tests tick "tried existing tests".
  - `ApplyTidy` and `Rebase` move Git refs inside `Store.Update`. `ApplyTidy`'s own comment says why: the transaction serializes checkpoint numbers.
  - Tidy undoes its refs on any commit error, including an uncertain one.
  - A checkpoint has no base.
  - `update --json` has no upstream comparison.
  - The v3 submit path never sets `forge.PullRequestInput`'s `ActionID` or `ExpectedRemoteHead`.
- **Its measurements hold.** `engine` is 30 files, 9,329 lines, and 29 local imports. Three providers import `engine`. Base, Xcode, and golden image names are built in four places. 24 CLI-reachable packages import `record`; the review counted 23.
- **It corrected the previous roadmap.** The line added on 2026-09-26 listed `assess`, `state`, and `workflow` for removal, but `tools/survey` runs on `assess`, and `tools/stateperf` on the other two. `architecture.md` now says so too.
- **Something it didn't say.** v3's submit already finds an existing pull request on its head branch before opening one, so a lost creation reply is recovered on the next run. Nothing tests that, nor v2's other recovery promises, so porting them as tests leads item 5.

## The new roadmap

The previous roadmap was 261 lines, most of them v2's history: its completed-capabilities table, carried organization work in `workflow` and `app`, and the triage of eight reviews. It moved whole to [v2/roadmap.md](../v2/roadmap.md), with its links fixed for the new place. `v2/README.md` names it.

The [new roadmap](../roadmap.md) has these sections:

- **Where v3 stands.**
- **Next, in seven ordered items:**
  1. what a check means;
  2. one plan per environment;
  3. history transitions;
  4. seams in the engine;
  5. retiring v2;
  6. reuse and archives;
  7. coverage.
- **The work that runs alongside on the Mac:** the oracle, host independence, and the survey's parallelism.
- **Smaller items.**
- **Three decisions for the person:** results under mixed test policies, the modelled tools profile, and Tahoe's Xcode bound.
- **Later, Maintenance, and Deferred.**
- **A record of what the review contributed.**

Every item the previous roadmap left open is placed:

- **Its steps 6, 8, 9, and 13** are the host-independence foundations, the oracle's remaining phases, reuse and archives, and coverage.
- **Step 7's v2 removal** is item 5, re-scoped.
- **Step 10's `create` registries and the prefix provider** are in coverage and Later.
- **The survey's unexplained parallelism** runs alongside on the Mac.
- **Its Later items:** expiring credentials, and evidence across registrations.
- **Its Deferred list** carries over.

Two groups were dropped as v2's:
- **Its carried organization work,** in `workflow` and `app` and the command pipeline over `r.build`, which the rebuild made moot.
- **Its Later items about v2's contributions, changesets, and GitHub verification,** whose capabilities v3 has in other forms.

This session's open ends are among the smaller items:
- progress for `outdated`, and one `vercmp` interpreter per port;
- parallel Tart releases;
- the Xcode archive signature;
- the Tart workarounds and when each can go;
- the pid-reuse proof, and the flake seen once.

## Where it departs from the review

- **Finding 3's remedy.** The history fix uses a per-branch lock, Git changes first, and a re-read of an uncertain commit, rather than a durable operation record.
- **Finding 4's timing.** Extraction waits until each piece is fixed. The exception is the provider contract, which moves first because it removes the providers' upward dependency on `engine`.
- **Finding 5's rank.** Per-runner GitHub evidence is a smaller item, since GitHub isn't the default provider. The port reader's evaluation report goes to item 6, where reuse needs it.
- **Finding 6's scope.** It shrinks to the JSON gap and the chosen release's provenance.
- **The reuse probe.** It waits on decision D1, where the recommendation is per-port reporting rather than blocking.
