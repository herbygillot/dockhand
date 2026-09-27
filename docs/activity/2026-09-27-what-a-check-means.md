# 2026-09-27: what a check means, from provider to pull request

Roadmap item 1: the defects the [architecture and data-flow review](../reviews/2026-09-27-architecture-and-data-flow.md) reproduced at the boundary between checking and publishing, fixed in place. Each piece is its own commit. The review's probes are regression tests, with the reuse probe rewritten for the person's decision on D1.

## One judge for test policy

- **Before.** Tart's guest program applied `--tests required`: a port whose tests failed or timed out failed at the test phase. GitHub's workflow reports the same facts, a build that passed and tests that failed, but nothing applied the policy to them. So `check --on github --tests required` passed a port whose tests failed.
- **Now.** `engine.Judge` applies the check's policy to every result where the runner records it, whichever provider built it:
  - under `required`, tests that failed or timed out fail a port that built, at the test phase;
  - a port that declares no tests passes under any policy;
  - the tests' own outcome is always kept.
- **Tart's guest still applies the same rule while it builds.** That is how it knows not to build a port against a dependency that failed its required tests. The guest's review test holds that blocking. The engine records every verdict, so the two can't disagree, and the guest protocol is unchanged.
- **GitHub runs its own tests.** The workflow is MacPorts' own, so `--tests skip` can't stop them. The provider says so (`OwnTestsProvider`), and the plan adds the line "github runs its workflow's own tests; with --tests skip they run there, and don't count".
- **Timed-out tests read as timed out:**
  - a port that built reads "✓ build passed; tests timed out (advisory)", in check output and in the pull request's table alike;
  - under `required` it reads "✗ failed at test: tests timed out";
  - the pull request's checklist no longer ticks "tried existing tests" when any timed out.
- **A result keeps its own check's policy** (D1, the person's decision). Evidence knows the policy of every check its results came from.
  - A result from an earlier check whose policy differs from the latest's reads under its own, and names that check: "✓ build passed; tests failed (advisory, check-3)".
  - It doesn't block a submission.
  - The review's probe expected a block. It is rewritten to assert the decided behavior.
- **One place words a result.** The terminal and the pull request call `Evidence.Words`, which knows each result's check and policy. The three callers of `TargetWords` go through it, and `TargetWords` is now the unexported `targetWords`.

Tests:
- the judge over every policy and outcome;
- wording for timed-out tests and `--tests skip`;
- a result that reads under its own check's policy;
- GitHub's required tests failing a check through the real Actions adapter with its fixtures, and its `--tests skip` note in the plan;
- the checklist and table with timed-out tests;
- an earlier advisory result standing after a required check of another port, without blocking the submission, with its check named in the pull request.

## A port an environment doesn't define isn't built there

- **Before.** The planner evaluates each environment separately, but only recorded an exclusion for a port the evaluation returned and ruled out. A subport that one release or architecture's evaluation never defined still went into the union of targets, and so into every environment's job.
- **Now.** A port an environment didn't define gets an exclusion there, with the platform in its reason: "not defined on macOS 26 arm64". The plan lists it as `Excluded`, the job leaves it out, and the evidence doesn't require it there. A port defined nowhere is still not planned. The full per-environment plan is roadmap item 2; this closes the defect in the current representation.
- **Test:** the review's probe, with a reader that defines an Intel subport only on x86_64. It's planned there, and excluded on arm64 with that reason.

## A baseline is planned the way a check is

- **Before.** A baseline copied the checked plan's targets and dependencies onto a revision of the branch's current base.
  - What they needed was the branch's evaluation, not master's.
  - The plan recorded no exclusions and no unmet needs. So a port that needs Xcode where there are only the Command Line Tools was sent there, and a subport excluded on a platform was built there.
  - The base was the branch's base now. After a rebase, that is a master the check never started from.
  - Without `--only`, it took every failed port, including those that failed at lint, fetch, or checksum.
- **Now.** `PlanBaseline` reads the checked revision's own base, and plans the ports at it through `PlanCheck`, in the check's environments and under its test policy.
  - Master's Portfiles are evaluated in each environment, so their exclusions, dependencies, and needs are master's, and unmet needs are recorded as a check's are.
  - It builds only the ports it names (`PlanRequest.alone`), not the rest of their directories' subports. It passes the directories it knows from the check, so they aren't looked up again by name.
- **Which ports, without `--only`** (`BaselineWorthy`):
  - those that failed at install or test somewhere;
  - not those that failed only before building, at lint, fetch, or checksum. Those failures come from the branch's own Portfile and distfiles, which master can't speak to. The baseline names them as left out, and `--only` builds them anyway;
  - as before, not a port the branch adds.
- **A directory is evaluated once, however many of its ports are named.** Naming two subports of one directory, with `--also` or in a baseline, evaluated it twice and counted its exclusions twice. A subport excluded on one of two platforms then counted as excluded everywhere and was dropped, and one excluded everywhere was kept.
- **Output.** The baseline's heading names the master it builds at, the checked base. `check --help` and the usage guide say how it plans and what it leaves out.

Tests:
- the review's probes, as a baseline that keeps each environment's Xcode needs and builds only the ports named, with an exclusion counted once for a directory named twice;
- a baseline after a rebase that builds at the checked base;
- which failed ports a baseline takes.

## A failed check points to the baseline

The person decided that baselines stay off by default, and that a failed check says which command builds its ports at master (design-v3.md §6.8).

- **The hint.** A failed check ends with `To see whether jq fails at master 1a2b3c4 too: dockhand check --baseline --branch jq-update`. It names the ports and the master the check started from. It names the branch as `status`'s hints do, since `wait` reports a check from anywhere.
- **When it appears.** Only when a baseline can answer something (`engine.BaselineCandidates`):
  - for a port that failed at install or test (`BaselineWorthy`), not only at lint, fetch, or checksum;
  - for a port master has at that base, not one the branch adds;
  - when the check is the branch's newest finished one, the one `--baseline` looks into. `wait` on an older check doesn't point to it.
- **`check.baseline = true`** runs the baseline the check pointed to, after "check.baseline runs it now:", and none when it pointed to none. Before, it ran one after any failure, and printed PlanBaseline's error when nothing qualified.
- **A shared lookup.** `PlanBaseline` and `BaselineCandidates` share `latestCheck` and `atBase`, so what the hint names is what `--baseline` builds. The ports are found among the check's own results.

Tests:
- the hint after a failed check, and `check.baseline` running it after the hint;
- no hint, and no automatic baseline, for a port that failed at fetch, whose `--baseline` names why;
- no hint from `wait` on a check that is no longer the newest;
- the candidates of a full check and of a narrowed one after it, and a baseline of the narrowed one.

## Restoring a rebase puts back its base and files

- **Before.** A rebase moved the branch's base, but its checkpoint kept only the heads, so `restore rebase-4` put the history back and left the base at the newer master. The review's probe showed the result: the next tidy failed with "is not above its base".
- **Something the review missed.** Restore never touches the working files, which is right for a tidy, whose files are the person's edits. A rebase's files are the rebased commit's, though. After restoring one, master's newer files read as the branch's uncommitted edits: a check would build those ports as changed, and a tidy would commit master's changes into the branch. Restoring only the base would have made checks worse, since they'd count the difference from the older master.
- **Now:**
  - **A checkpoint keeps the base before and after** (schema 13, `base_before` and `base_after`). A rebase records the master it moved from and to; a tidy records the base twice, since it doesn't move it. `Checkpoint.Validate` requires both, equal for a tidy.
  - **A rebase records its checkpoint and the branch's new base in one transaction,** so the two can't disagree. Before, a separate transaction set the base afterwards. Where Git changes happen relative to the transaction is roadmap item 3.
  - **Restoring a rebase moves the checkout back:** the branch, index, and files, from the rebased head to the old one (`git.MoveCheckout`, which runs `git reset --keep`). It follows the sparse checkout. A local change to a file it would put back, or an untracked file in the way, stops it and changes nothing.
  - **Then it sets the branch's base back** to the checkpoint's `BaseBefore`, in the transaction that marks the checkpoint restored.
  - **A rebase checkpoint made before schema 13** has no base. Restoring it puts back the history and files, and says the branch still counts from the newer master until `dockhand rebase` puts its commits there.
  - **Output.** `restore` says "Restored dockhand/notes to its history and files before rebase-2 (1a2b3c4), on master 5d6e7f8 again." A tidy's restore reads as before. `--json` adds the branch's base.

Tests:
- the review's probe, extended: after a rebase and its restore, the head, the base, and a clean worktree, and a tidy that plans;
- a local change to a file the restore would put back stops it, changing nothing;
- `MoveCheckout` in a sparse checkout: it moves the branch and files back, keeps a change to a file it doesn't move, leaves paths outside the checkout outside, and refuses a conflicting local change or a checkout at another commit;
- the store keeps a checkpoint's base, and refuses a tidy that moves it;
- `restore` of a current rebase checkpoint and of one without a base.

## update --json carries the upstream comparison

- **Before.** `update` printed what comparing the old and new source archives found, and stored it with the edit, but `update --json` left it out. A script driving updates couldn't see a changed license or a new dependency.
- **Now.** The result has `upstream`: its `changes`, each with `kind`, `path`, `message`, and `hold`; a `problem` when the archives couldn't be compared; and `held`, whether any change holds the update for a look before `serve` submits it. It is absent when nothing was compared, as for a port fetched with Git. The view has its own types, like the command's other results, so the stored form can change without changing the scripting contract.

Tests:
- `update --json` with archives that couldn't be fetched carries the problem;
- the whole loop's `update --json`, which has no archives, carries none.
