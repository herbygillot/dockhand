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

1. **What a check means, from provider to pull request.** Done 2026-09-27, in six commits ([note](activity/2026-09-27-what-a-check-means.md)):
   - one judge applies the test policy whichever provider built, and timed-out tests read as timed out;
   - a port an environment doesn't define isn't built there;
   - a baseline is planned the way a check is, at the base of the check it explains, and builds only the ports it names;
   - a failed check names the baseline command when one can help;
   - a checkpoint records the branch's base, and restoring a rebase puts back its base and its files;
   - `update --json` carries the upstream archive comparison.

   The review's seven probes are regression tests, with `ReuseHonorsTestPolicy` rewritten for D1. Two defects the review didn't name turned up and were fixed with them:
   - restoring a rebase left master's newer files as the branch's uncommitted edits;
   - naming two subports of one directory, with `--also` or in a baseline, counted the directory's exclusions twice.

2. **One plan per environment.** Done 2026-09-27 ([note](activity/2026-09-27-one-plan-per-environment.md)):
   - each environment has its own plan, found by the whole environment: its build order, dependencies, Xcode needs, unmet targets, and exclusions with their reasons;
   - the branch's targets and the person's selection stay the plan's own;
   - each environment builds in its own order, so opposite dependencies on two releases are no longer refused;
   - `engine.Counts` is the one rule for which earlier result of the same files stands;
   - a baseline rebuilds a port only where it failed;
   - plans recorded in the old form read as per-environment plans.

3. **History changes as complete transitions.** Done 2026-09-27 ([note](activity/2026-09-27-history-transitions.md)):
   - `tidy`, `rebase`, and `restore` hold the branch's lock throughout, and no transaction calls Git;
   - a tidy or rebase records its checkpoint as prepared, makes its Git change, and settles it; the next history change on the branch finishes what a stopped one left, from what Git shows;
   - an uncertain commit is read back, and a Git change once made is never undone;
   - a rebase replays its commits before anything moves (`git.Replay`).

   It departs from this roadmap's "no durable operation record": the next command can only finish a stopped change if its intent was recorded first. The prepared checkpoint is that record, one state column rather than a workflow engine.

4. **Seams in the engine.** Done 2026-09-27, but for what waits on item 6 ([note](activity/2026-09-27-engine-seams.md)):
   - the provider contract is `internal/buildenv`, which the providers import instead of `engine`, and a test keeps them off the engine;
   - a boundary test names every package `engine` may import, with why, and fails on one unlisted or no longer imported;
   - history's transitions are `internal/history`, with the verbs left in the engine;
   - the test-policy judge is `model.TestPolicy.Judge`.

   **Per-environment planning and the rule for which results count stay in the engine until item 6,** which changes both: the planner's input becomes the port reader's evaluation report, and reuse keys results by what each build consumed. They move with it, once.

   **Not split:** `command`, whose size is its job, `portedit`, which already delegates, and the CLI verbs into packages of their own.

5. **Retire v2.** Done 2026-09-27 ([note](activity/2026-09-27-retire-v2.md)):
   - v2's recovery promises for publishing are v3 tests, and the one v3 lacked is built: a pull request is read back when opening it fails, so a lost reply or two racing submits end with one;
   - `tools/stateperf` is retired, `verify/staging` is `buildenv/staging`, and `assess` is part of `tools/survey`;
   - `workflow`, `state`, `publish`, `verify`, `git/changeset`, and `macports/dependents` are deleted, 35,477 lines, with the functions only they called;
   - `record`'s live types moved to `model`, `forge`, and `macports/commitmsg`, and `record` is deleted.

   The order changed from the one planned: settling the three things outside v2 that used it let v2 go whole, before `record` moved. The planned check against new imports of retired packages was then moot.

6. **Reuse and archives** (decisions 28 and 44; the previous step 9). This builds on item 2's predicate, and takes planning and that predicate out of the engine as it changes them (item 4).
   - **Per-target reuse** by recorded observations, negative ones included. Begun 2026-09-27 ([note](activity/2026-09-27-reuse-and-archives.md)): where every target an environment would build is unchanged in what it read, its earlier passed results are reused and nothing is built (`check --fresh` builds). Reusing some and building others waits for the kept archives below.
   - **What each build records:** its input identity and the digest of every archive it consumed. Done 2026-09-27 for Tart ([note](activity/2026-09-27-reuse-and-archives.md)): images keep each port's archive (setup protocol 3), and each result names its inputs by content. Those are the ports active as it built, with their archives' digests and directories, plus the target's own directory and `_resources` by tree, and the environment. It also keeps its own archive's digest.
   - **Environment identity by origin** (32). Done 2026-09-27 for Tart ([note](activity/2026-09-27-reuse-and-archives.md)): setup pins the vanilla image by digest and records each image's origin on the host. Each provider run records its environment's identity, and a result counts only while the environment is still that one. The other providers say nothing yet, so their results stand as before.
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

- **Tart workarounds to retire as Tart releases fixes:**
  - the retry of a listing that raced a delete ([openai/tart#1353](https://github.com/openai/tart/issues/1353));
  - trusting a delete only by the VM's absence (#1345, fixed by #1350 on 2026-09-26, hours after 2.39.0 was tagged: unreleased as of 2026-09-27);
  - guests reached over SSH, never `tart exec` (#1346); setup's agent readiness probe is the last `tart exec`, and could move to SSH.
- **A flake to watch.** `TestTidyAsksWhatItCannotKnow` once failed in its cleanup with a directory not empty ([note](activity/2026-09-27-stopped-checks.md#seen-once-not-explained)).

## Decisions for the person

- **D2. Tools or Xcode profile.** Should modelled contexts use the Xcode profile, as MacPorts' builders do, or the tools profile they use now? This has been open since oracle phase 5, and changes nothing an update edits today.
- **D3. Tahoe's Xcode.** Tahoe's Xcode has no upper bound, so a `--rebuild` of its Xcode image would now choose Xcode 27, by the rule that gives Sequoia 26.3. Should Tahoe stay on 26?

### Decided

- **D1. Results checked under different test policies** (2026-09-27). Each result keeps the policy it was checked under, and reads under it, naming its check where that differs: "tests failed (advisory, check-3)". A later `--tests required` check of some ports doesn't bind the others, nor block the submission. The review's probe expected a block.
- **Baselines** (2026-09-27).
  - They stay optional and off by default: `--baseline`, or `check.baseline = true`.
  - A failed build, install, or test of a port the base has names the command.
  - A baseline is always dockhand's own build. MacPorts' buildbot history is never used as one, nor shown beside one, which withdraws decision 20's labeled hint.
  - Baseline results stay in the check's output. Carrying them into the pull request can come back if it's wanted.
- **Xcode images follow MacPorts' buildbots** (2026-09-27, [note](activity/2026-09-27-xcode-follows-the-buildbots.md)). A release's Xcode image has the Xcode its arm64 buildbot runs, from the facts table, as its Command Line Tools already follow the builder (decision 13). `providers.tart.xcode` in the configuration names another for a release. It replaces "the newest Xcode the release runs", which gave every release but Golden Gate a newer Xcode than MacPorts builds with.
- **`bump` takes a port to its pull request asking nothing** (2026-09-27, [note](activity/2026-09-27-bump.md)). It is `update --new --submit --yes` as a command of its own, the one-shot beside `update`'s step at a time. Nobody looks before it submits, so it holds what `serve` holds, and another open pull request for the port now holds both. The name was kept knowing `port bump` refreshes checksums, and v2's `bump` stopped at the edit.
- **Branch worktrees go in `~/Source/macports-branches`** (2026-09-27, [note](activity/2026-09-27-worktree-root.md)), wherever the clone is, rather than beside it. `worktrees` in the configuration still names another.
- **xcodes downloads a missing Xcode** (2026-09-27, [note](activity/2026-09-27-xcode-follows-the-buildbots.md)). With xcodes installed, setup downloads the Xcode it's missing: at a terminal it asks first, and without one it uses the sign-in xcodes keeps, showing what xcodes said when it fails. It checks the archive with `pkgutil --check-signature` before taking it. An exception to decision 32, chosen knowingly: dockhand uses only xcodes' documented command, but xcodes signs in through Apple's undocumented sign-in and download endpoints. When Apple changes them, the download fails loudly, and setup still names the Xcode to download by hand.

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
- **Finding 3's remedy.** A general operation record is heavier than needed: the checkpoint, recorded as prepared before the Git change, is the record, beside a per-branch lock and reading an uncertain commit back. (Settled while building item 3; the roadmap first said no record at all, which couldn't make a stopped change recoverable.)
- **Finding 4's timing.** Extraction comes after each piece is fixed, apart from the provider contract, which goes first.
- **Finding 5 is ranked lower.** GitHub isn't the default provider, so per-runner evidence is a smaller item. The port reader's report joins item 6, where reuse needs it.
- **Finding 6 is narrowed.** It shrinks to the JSON gap and the release's provenance, until something reads more.
- **Finding 1's reuse probe was rewritten for D1.** It expected a block; the person decided each result keeps its own check's policy.

**Earlier reviews** were triaged in the previous roadmap, which records what each contributed and what was declined ([v2/roadmap.md](v2/roadmap.md#review-triage-and-validation)).
