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
