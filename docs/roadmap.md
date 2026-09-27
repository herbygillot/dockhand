# Dockhand roadmap

This is the source of truth for what gets built next and in what order. [Design v3](design-v3.md) defines behavior, [architecture](architecture.md) maps it onto the code, and [usage](usage.md) is the guide. Activity notes record what was done and how it was proven; they are not queues, and neither are reviews: a review's claims are checked against the code, and what holds is folded into the items below.

Reconciled 2026-09-27 against three sources.
- **The [architecture and data-flow review](reviews/2026-09-27-architecture-and-data-flow.md).** Its seven probes were run at `52d03e2a`, and every one fails as the review says; its claims were checked against the code. What was taken from it, and what was changed, is under [Reviews](#reviews).
- **The previous roadmap.** It carried the rebuild's thirteen steps and v2's history, and is kept whole as [v2/roadmap.md](v2/roadmap.md). Every item it left open is below.
- **The work of 2026-09-26 and 27.** That is a real update of five ports, faster `outdated`, stopped checks and the clones they leave, and Golden Gate. The notes are linked where they bear on an item.

## Where v3 stands

The whole loop is built and has been used for real work:

- **Branches:** `init`, `start`, `adopt`, `adopt --pr`, and `path`.
- **Authoring:** `update` with `--outdated` and `--revbump-dependents`, `checksums` with stealth updates, `revbump`, `create` for GitHub projects, and `edit`.
- **Checks:** `check` on Tart, on your fork's GitHub Actions, or on your own script. Tart covers macOS 12 through 27, each with an Xcode add-on. There are also `logs`, `retry`, `--baseline`, `queue`, `wait`, and `cancel`.
- **Shaping and submitting:** `tidy` and `restore`, `submit` with `--check` and `--passing`, and `review`.
- **Following:** `status`, `watch`, and `serve` with its daily look at your ports, and `clean` and `archive`.

Five one-port updates were submitted with it and merged on 2026-09-26 ([note](activity/2026-09-26-real-update.md)), and a two-port branch was checked on two releases. Golden Gate images, base and Xcode, were made and checked on 2026-09-27 ([note](activity/2026-09-27-golden-gate-images.md)).

The weak point the review found is the information passed between steps. Several places reduce a richer fact to a name, a flag, or one verdict, and later code guesses at the rest. Some of those reductions already give wrong answers. That is where Next begins.

## Next

In order. Each item lands in its own commits with an activity note, and a review's probe becomes a regression test when its item fixes what it probes. The order is the implementer's to re-settle as work lands.

1. **What a check means, from provider to pull request.** These are defects, fixed in place before anything moves.
   - **One judge for test policy.** Providers report what happened, the build's outcome and the tests' outcome, and one function in the engine decides the verdict under the plan's policy. Today Tart's guest program applies `--tests required` and the github provider never reads it, so `check --on github --tests required` passes a port whose tests failed. A provider that can't honor a policy says so: GitHub's workflow runs its own tests and can't skip them.
   - **Timed-out tests read as timed out.** Today they show as ✓ in the results and the pull request's table, and tick "tried existing tests" in its checklist.
   - **A port absent from one environment's evaluation isn't built there.** A subport that one release or architecture doesn't define is excluded there, with the reason, rather than inherited from another environment.
   - **A baseline is planned the way a check is.**
     - It starts from the base of the check it explains, not the branch's current base after a rebase.
     - It keeps each environment's exclusions, and what an environment can't meet: a port that needs Xcode isn't sent to a Command Line Tools image.
   - **A failed check names the baseline command when one can help.** That is when the port exists at the base and failed while building, installing, or testing. The check says, for example, "To see whether harbor-viewer fails at master 4c1e2d0 too: dockhand check --baseline". There is no line for a lint, fetch, or checksum failure, nor for a port the branch adds (the decision below).
   - **A checkpoint records the branch's base, and `restore` puts it back.** After `rebase` then `restore`, the base stays at the newer master today, and the next `tidy` fails.
   - **`update --json` carries the upstream archive comparison** its text already shows.

   The review's seven probes are the tests: `GitHubHonorsRequiredTests`, `TimedOutTestsAreNotPassing`, `AbsentSubportIsNotBuilt`, `BaselineKeepsEnvironmentRequirements`, `BaselineUsesCheckedBase`, `RestoreRestoresBase`, and `ReuseHonorsTestPolicy`. The last one's expected answer waits on decision D1.

2. **One plan per environment.**
   - **What a plan holds for each environment.** An environment's plan holds, keyed by the whole environment rather than by position:
     - the ports its evaluation defined, and why each other one isn't built there;
     - what each port needs there, such as Xcode;
     - its dependency graph and its build order.
   - **What stays branch-wide.** The branch's changed scope and the person's `--only`/`--also` selection stay apart from those.
   - **A baseline rebuilds a port only in the environments where it failed.**
   - **Each environment builds in its own order.** That retires today's refusal, where dependencies that run opposite ways on two releases make a cycle in the combined graph.
   - **One predicate says which recorded results count toward a submission:** matching selection, environment, and test policy. Checks and baselines are then planned through the same path, which item 1 begins.

3. **History changes as complete transitions.** This covers `tidy`, `rebase`, and `restore`, and the review's reproduced base bug is fixed in item 1.
   - **A per-branch lock.** Tidy and rebase move Git refs inside a database transaction today, using it as a lock, against the store's own rule that transactions never touch Git. A per-branch lock takes that job instead.
   - **Git first, then the record.** Git's changes go first, guarded by compare-and-swap as they are now, then the record.
   - **An unknown commit is read before it is undone.** A commit the store reports as uncertain is read back before anything is undone. Today tidy undoes its refs on any error, including a commit that may have landed.
   - **A crash is recoverable.** A crash between the Git change and the record leaves a state the next command recognizes and finishes.
   - **Tests restart it and make the commit uncertain.** This does not bring back v2's general workflow engine.

4. **Seams in the engine.**
   - **The provider contract moves first.** `Job`, `Build`, and `Fork` move to a leaf package that providers import instead of `engine`; all three providers import `engine` today. This is cheap, and it removes the upward dependency that would otherwise tangle the next moves.
   - **Then the pieces fixed in items 1 to 3 move out** behind the `Engine` facade: the test-policy judge, per-environment planning, and history. Each moves once it is fixed and stable, not before.
   - **A boundary test limits what `engine` itself may import**, as the command boundary test limits `command`.
   - **Not split:** `command`, whose size is its job, `portedit`, which already delegates, and the CLI verbs into packages of their own.

5. **Retire v2.** The binary uses none of v2's orchestration, but some of it is still live:
   - about two dozen live packages import `record`;
   - the Tart provider imports `verify/staging`;
   - `tools/survey`, the oracle's survey, runs on `assess`;
   - `tools/stateperf` runs on `state` and `workflow`.

   The previous roadmap's "remove the v2 packages" missed the last three. So, in order:
   - **v2's recovery promises, as v3 tests.** A pull request whose creation reply was lost is found and updated, not opened twice; submit already looks for one on the head branch, untested. A push is refused when the fork's branch moved. A person's edited description is kept. Anything v3 lacks is built then.
   - **`record`'s live types move to their owners.** They go to `model` and `forge`, with a small neutral release contract where a move would make a cycle. Retired records are not moved into `model`. `forge.PullRequestInput` loses the v2-only fields v3 never sets.
   - **`verify/staging` moves to a neutral home.**
   - **`assess` moves under `tools/survey`, or stays on purpose.** `tools/stateperf` is retired.
   - **A dependency check refuses new imports of the retired packages.**
   - **Then the deletion:** `workflow`, `state`, `publish`, `verify` apart from staging, `git/changeset`, and `macports/dependents`. Their timing-sensitive tests go with them ([note](activity/2026-09-27-golden-gate-images.md#found-not-done)).

6. **Reuse and archives** (decisions 28 and 44; the previous step 9). This builds on item 2's predicate.
   - **Per-target reuse** by recorded observations, negative ones included.
   - **What each build records:** its input identity and the digest of every archive it consumed.
   - **Environment identity by origin** (32).
   - **Archives ready for dependents** only once durably transferred and checked.
   - **The port reader returns an evaluation report,** or a reference to its observation, rather than port names and dependency lists, so the observations can be recorded; today they would have to be reconstructed.

7. **Coverage** (the previous step 13, with what `outdated` found).
   - **The 144 ports `outdated --mine` can't check,** sized by reason first. Most use Portfile conventions discovery doesn't take ([note](activity/2026-09-27-outdated-speed.md)).
   - **Files that preparation adds and deletes** (40).
   - **An outcome for the 441 ports with nothing to fetch.**
   - **Literal segments of composed versions,** llvm's and openjdk's.
   - **Smaller buckets:** the Go toolchain check on gitlab.com, and the R ports' condition.
   - **Re-sizing:** the host-reader buckets after the oracle, and the PortGroup inclusion map.
   - **`create`:** from registry names (`pypi:`, `crates:`, `go:`), `--like`, and `go.vendors`.

### Alongside, on the Mac

These need MacPorts Base, whole-tree surveys, or VMs, so they run as the Mac allows, independent of the order above.

- **The oracle's remaining phases** ([scope](oracle.md)):
  - phase 3, the workspace rule: reads inside the tree materialize on demand, and reads outside it are refused;
  - phase 6, the bootstrap under the environment contract;
  - `java_home` answered from the facts table;
  - 4b, `with-deps`, only when a survey shows a registry answer reaching what an update edits;
  - the comparison with a fresh Tart guest, possible since the v3 Tart provider;
  - decision D2.
- **Host-independence foundations** (the previous step 6):
  - the evaluator's clean launch environment (16);
  - the environment contract, and a disposable `portdbpath`;
  - normalized records;
  - `base212`, and a master preview identified by commit (39).
- **The survey's parallelism.** It ran on 7.2 cores on 2026-09-22 against 10.7 at the baseline, unexplained. A mutex and CPU profile over one category comes before the next whole-tree run.

## Smaller items

These are taken when their area is next touched, or between items.

- **One image descriptor.** Base, Xcode, and golden image names are built in four places; one descriptor in `internal/tart` gives the release, profile, prepared name, golden name, and source.
- **Xcode archive signatures.** Setup would refuse an archive `pkgutil --check-signature` doesn't call "signed Apple Software". Setup's own `xip --expand` says it doesn't validate the signature.
- **`outdated` shows progress** while it runs. It is three minutes of silence over 1,076 ports.
- **One interpreter per port for `vercmp`,** rather than a new `tclsh` for each of about five comparisons.
- **Tart checks of several releases in parallel,** within the Mac's two VMs.
- **Per-runner evidence for the github provider.** Its runners, one per macOS release, fold into one result today. Each should keep its release, result, and log.
- **Structured detail from Tart guests:** the failure detail and MacPorts version the guest already reports. A reported environment is also linked to the executions that supplied it.
- **Stored edits keep the chosen release's provenance,** its tag and upstream commit, with an update, reload, and `tidy`/`status` test. A fuller report waits until something reads it.
- **Tart workarounds to retire as Tart releases fixes:**
  - the retry of a listing that raced a delete ([openai/tart#1353](https://github.com/openai/tart/issues/1353));
  - trusting a delete only by the VM's absence (#1345, merged, unreleased);
  - guests reached over SSH, never `tart exec` (#1346); setup's agent readiness probe is the last `tart exec`, and could move to SSH.
- **The process-start reader's pid-reuse case, proven on a Mac.** A killed process was judged dead on 2026-09-27 ([note](activity/2026-09-27-stopped-checks.md)).
- **A flake to watch.** `TestTidyAsksWhatItCannotKnow` once failed in its cleanup with a directory not empty ([note](activity/2026-09-27-stopped-checks.md#seen-once-not-explained)).

## Decisions for the person

- **D1. Results checked under different test policies.** A branch's ports may have passed under different test policies: advisory in one check, `--tests required` in a later check of only some of them. What should `submit` do?
  - *Recommended:* each result keeps the policy it was checked under, and the pull request reports it per port. A later required-tests check of some ports doesn't bind the others retroactively.
  - *The review's probe:* block the submission.
- **D2. Tools or Xcode profile.** Should modelled contexts use the Xcode profile, as MacPorts' builders do, or the tools profile they use now? This has been open since oracle phase 5, and changes nothing an update edits today.
- **D3. Tahoe's Xcode.** Tahoe's Xcode has no upper bound, so a `--rebuild` of its Xcode image would now choose Xcode 27, by the rule that gives Sequoia 26.3. Should Tahoe stay on 26?

### Decided

- **Baselines** (2026-09-27).
  - They stay optional and off by default: `--baseline`, or `check.baseline = true`.
  - A failed build, install, or test of a port the base has names the command.
  - A baseline is always dockhand's own build. MacPorts' buildbot history is never used as one, nor shown beside one, which withdraws decision 20's labeled hint.
  - Baseline results stay in the check's output. Carrying them into the pull request can come back if it's wanted.

## Later

- **The prefix provider:** checks on a MacPorts installation on this Mac.
- **Expiring credentials.** This adds access and refresh tokens with their expirations, and refresh across processes, before any login flow that needs them.
- **Evidence across repository registrations.** One database serves several checkouts. Once reuse keys evidence by content (item 6), what's left is whether identical inputs from another checkout are trusted.

## Maintenance

- **MacPorts compatibility.** Evaluator tests pass on Base 2.12.6. Version adapters follow the [Base design](macports-base-design-prospective.md): the current release plus a pinned master preview. Keep Base and PortGroup compatibility distinct ([evidence](macports-compatibility.md)).
- **Images.** Twelve images are made and checked: base and Xcode for macOS 12, 13, 14, 15, 26, and 27. Re-probe the facts table when an image is rebuilt; the 2026-09-27 probes found every earlier row unchanged. Golden Gate needs Tart 2.39.0 or newer.
- **Package boundaries.** Extend import checks when touching a boundary that matters; the command boundary test, and item 4's engine boundary test, are the models. Don't split packages by size alone. Run `make deadcode` after a milestone, and keep test-only exports documented as such.
- **GitHub's budget.** Requests to its API are paced at 750 a minute, under its documented 900. A whole `outdated --mine` spends about 1,800 of the 5,000 an hour the person's account has, a budget shared with the GitHub CLI.
- **Test throughput.** The cost unit is the MacPorts interpreter. Keep per-test timing visible, and prefer simulated clocks for time-driven tests; v2's `workflow` tests show what wall-clock budgets do under load.

## Deferred

- Exporting state to Git notes, or rebuilding the database from Git metadata.
- Portable verification evidence between machines.
- A generic workflow graph or package-build scheduler.
- Changing branches by itself in response to reviews, CI failures, or merge conflicts.
- Broad provider matrices and an `all`-platforms mode. A list of releases a person names, each of which must pass, is supported.
- A QEMU provider without a concrete use, and direct use of Apple's Virtualization framework (decision 41); the provider interface stays open for either.
- Parity with v1 or v2 commands that have no current use.

## Reviews

**The [architecture and data-flow review](reviews/2026-09-27-architecture-and-data-flow.md)** of 2026-09-27 was checked against the code at `52d03e2a`.
- **Its probes:** all seven fail as stated.
- **Its measurements hold:** `engine` is 30 files and 9,329 lines, with 29 local imports; all three providers import it; `tools/survey` needs `assess` and `tools/stateperf` needs `state` and `workflow`.
- **Its claims hold,** in particular that tidy and rebase move Git refs inside database transactions.

Taken:
- **Findings 1 to 3,** as items 1 to 3.
- **Finding 4,** as item 4.
- **Finding 7,** as item 5. It corrected the previous roadmap, whose removal list would have broken the survey.
- **Finding 8,** as a smaller item.

Changed:
- **Finding 3's remedy.** A "durable operation record" is heavier than needed. A per-branch lock, Git first, and reading an uncertain commit back cover it.
- **Finding 4's timing.** Extraction comes after each piece is fixed, apart from the provider contract, which goes first.
- **Finding 5 is ranked lower.** GitHub isn't the default provider, so per-runner evidence is a smaller item. The port reader's report joins item 6, where reuse needs it.
- **Finding 6 is narrowed.** It shrinks to the JSON gap and the release's provenance, until something reads more.
- **Finding 1's reuse probe waits on D1.**

**Earlier reviews** were triaged in the previous roadmap, which records what each contributed and what was declined ([v2/roadmap.md](v2/roadmap.md#review-triage-and-validation)).
