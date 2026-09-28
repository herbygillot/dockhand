# Dockhand roadmap

This is the source of truth for what gets built next and in what order. [Design v3](design-v3.md) defines behavior, [architecture](architecture.md) maps it onto the code, and [usage](usage.md) is the guide. Activity notes record what was done and how it was proven; they are not queues, and neither are reviews: a review's claims are checked against the code, and what holds is folded into the items below.

Reconciled 2026-09-27 against four sources.
- **The [architecture and data-flow review](reviews/2026-09-27-architecture-and-data-flow.md).** Its seven probes were run at `52d03e2a`, and every one fails as the review says; its claims were checked against the code. What was taken from it, and what was changed, is under [Reviews](#reviews).
- **The previous roadmap.** It carried the rebuild's thirteen steps and v2's history, and is kept whole as [v2/roadmap.md](v2/roadmap.md). Every item it left open is below.
- **The work of 2026-09-26 and 27.** That is a real update of five ports, faster `outdated`, stopped checks and the clones they leave, and Golden Gate. The notes are linked where they bear on an item.
- **The [code-organization review](reviews/2026-09-27-code-organization-review.md)** of the same day. It read `a71fc67f`, and its findings were checked again at `3b16b187`, 23 commits later ([note](activity/2026-09-27-code-organization-review-reconciled.md)). What holds is the block before item 6, pieces of items 6 and 7, and smaller items; the rest is under [Reviews](#reviews).

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

**Before item 6 goes on: what the code-organization review found that shouldn't wait** ([note](activity/2026-09-27-code-organization-review-reconciled.md)). In order, each in its own commits.

- **This week's regressions.** Done 2026-09-27:
  - a check no longer fails when an earlier build it would reuse read a port outside the ports tree (finding 36);
  - what the engine and Tart assemble on first use is assembled once, under a lock, now that a check's environments build together (finding 1).
- **The guardrail unattended submission relies on.** `bump` and serve submit with nobody looking, and the upstream comparison that holds them never runs for a port with `go.vendors` or `cargo.crates` (finding 32).
  - Such an update keeps the old archives for the comparison, as other updates do.
  - What an update couldn't check travels as a typed fact, through a `preparation.Result` that embeds `portedit.Result` (findings 31 and 19). That covers an unpaired comparison, a Go toolchain minimum it couldn't rewrite, and patches it left unchecked.
  - Those facts hold an unattended submission (D4). Done 2026-09-27 ([note](activity/2026-09-27-what-couldnt-be-checked-holds.md)):
    - archives it couldn't compare hold `bump`'s and serve's submissions, as a changed license does;
    - a Go or Cargo port's update keeps its old archives, so its comparison runs;
    - the old archives are compared as MacPorts shipped them: checked against the Portfile's checksums, and from MacPorts' mirror under the port's `dist_subdir` where upstream now serves something else;
    - a Go toolchain minimum left below go.mod's requirement holds;
    - unchecked patches are shown, not held, since the build applies them.
- **Rules the design promises and the code doesn't keep:**
  - one check per branch, enforced in `Enqueue`, which `submit --check`, `update --submit`, and `retry` bypassed; `--tests` checked before evaluation; and a test for submit's commit binding (finding 2). Done 2026-09-27 ([note](activity/2026-09-27-one-check-per-branch.md));
  - capture's moved-while-read check with `--include` (finding 43). Done 2026-09-27;
  - `create`'s result when interrupted, and `adopt`'s and `rebase`'s counts (finding 33). Done 2026-09-27;
  - a new branch whose record's commit was uncertain is read back, not undone (finding 24). Done 2026-09-27;
  - a canceled script build stops its whole process group, gently first (finding 42). Done 2026-09-27;
  - `serve --install` carries `--git`, `TART_HOME`, and the `DOCKHAND_*` settings it was installed under, never a token (finding 46);
  - `ExitCode` finds an exit through wrapping (finding 17). Done 2026-09-27 ([note](activity/2026-09-27-promised-rules.md)).
- **Progress on stderr** (finding 31):
  - `-v` shows what the 48 progress reports say;
  - `outdated` prints what it found when interrupted, and clears its count.
- **The journal and serve's files:**
  - cleanup prunes old events and ended sessions (finding 34);
  - `check` and `watch` read the journal from where they start (finding 34);
  - each command opens one observer session (finding 34);
  - serve's and cleanup's stamps and `serving.json` are per repository, and the day's look is stamped after it (finding 35);
  - `CleanupDue` gives its reason as a type (finding 37).
- **Dead code:**
  - about 460 lines of `git`;
  - `commitmsg`'s unused composer and `outdated`'s unused helpers;
  - the other uncalled pieces the review and its check found (findings 18, 9, 20, 13, and 38).

The order puts live regressions first, then the guardrail unattended submission relies on, then small promises the code doesn't keep, then what a person watching sees, then what grows without bound, and dead code last, since removing it changes nothing.

6. **Reuse and archives** (decisions 28 and 44; the previous step 9). This builds on item 2's predicate, and takes planning and that predicate out of the engine as it changes them (item 4).
   - **Per-target reuse** by recorded observations, negative ones included. Begun 2026-09-27 ([note](activity/2026-09-27-reuse-and-archives.md)): where every target an environment would build is unchanged in what it read, its earlier passed results are reused and nothing is built (`check --fresh` builds). Reusing some and building others waits for the kept archives below.
   - **What each build records:** its input identity and the digest of every archive it consumed. Done 2026-09-27 for Tart ([note](activity/2026-09-27-reuse-and-archives.md)): images keep each port's archive (setup protocol 3), and each result names its inputs by content. Those are the ports active as it built, with their archives' digests and directories, plus the target's own directory and `_resources` by tree, and the environment. It also keeps its own archive's digest.
   - **Environment identity by origin** (32). Done 2026-09-27 for Tart ([note](activity/2026-09-27-reuse-and-archives.md)): setup pins the vanilla image by digest and records each image's origin on the host. Each provider run records its environment's identity, and a result counts only while the environment is still that one. The other providers say nothing yet, so their results stand as before.
   - **Archives ready for dependents** only once durably transferred and checked.
   - **The port reader returns an evaluation report,** or a reference to its observation, rather than port names and dependency lists, so the observations can be recorded; today they would have to be reconstructed.
   - **From the code-organization review,** as planning and results move:
     - `PlanCheck`'s phases named (finding 4);
     - a kind on each cell of the evidence, and one rule for an `--also` extra, which status and submit read differently today (finding 25);
     - the evaluator's computed facts as typed fields (finding 27);
     - the tests vocabulary checked where results are written, since reuse carries results on (finding 28);
     - an identity that can't be read fails the attempt, rather than recording "no origin" (finding 39);
     - the release `outdated` found passed to the update, and a plan's `--only`, `--also`, `--fresh`, and omissions in its JSON (finding 36);
     - one set of dependencies given to `engine.Open` (finding 1).

7. **Coverage** (the previous step 13, with what `outdated` found).
   - **The 144 ports `outdated --mine` can't check,** sized by reason first. Most use Portfile conventions discovery doesn't take ([note](activity/2026-09-27-outdated-speed.md)).
   - **Files that preparation adds and deletes** (40).
   - **An outcome for the 441 ports with nothing to fetch.**
   - **Literal segments of composed versions,** llvm's and openjdk's.
   - **Smaller buckets:** the Go toolchain check on gitlab.com, and the R ports' condition.
   - **Re-sizing:** the host-reader buckets after the oracle, and the PortGroup inclusion map.
   - **`create`:** from registry names (`pypi:`, `crates:`, `go:`), `--like`, and `go.vendors`.
   - **From the code-organization review,** where this work touches:
     - one livecheck pipeline for upstream's two paths, before discovery changes (finding 6);
     - `create` names `adopt` as the other authoring commands do, and records design v3's subject (findings 8 and 9).

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
- **The code-organization review's smaller findings,** each when its files are next touched. Findings are the [review](reviews/2026-09-27-code-organization-review.md)'s, as the [note](activity/2026-09-27-code-organization-review-reconciled.md) corrects them.
  - **Tart:** one SSH wait that stops at a refused login, and a refusal the runner doesn't retry: about twelve minutes today (finding 7).
  - **Serve:** its workers each say a problem once, and its daily look runs off the loop (finding 3).
  - **Copies to fold:**
    - submit's phases, keeping `--accept`'s errors (finding 5);
    - run recipes (finding 11);
    - tidy's rules adapter (finding 12);
    - GitHub remotes (finding 13);
    - environment words and provider names (finding 14);
    - one table test for exec admission (finding 15);
    - `forge/github`'s guards (finding 16);
    - small helpers (finding 17);
    - a port directory's rule (finding 29);
    - a pull request's head, and its 404 (finding 38).
  - **JSON:**
    - status's branch embeds the reference other commands give (finding 10).
  - **Structure:**
    - `macports`' program and platform mechanics in a subpackage (finding 21);
    - the history transition as one `Make`, keeping submit's merge check (finding 26);
    - `Update`'s and `PlanTidy`'s seams (finding 44).
  - **Latent:**
    - an edit record whose commit was uncertain is read back, as a new branch's is: `Update` and `Create` write their files first, and holds are read from the records (finding 24);
    - a stealth update's edits evaluated again (finding 19);
    - `ls-remote` in a fresh scratch directory (finding 30);
    - the index cache's identity off a Mac (finding 40);
    - store error kinds documented (finding 41);
    - anonymous GitHub remembered for a few minutes (finding 45).
- **A flake to watch.** `TestTidyAsksWhatItCannotKnow` once failed in its cleanup with a directory not empty ([note](activity/2026-09-27-stopped-checks.md#seen-once-not-explained)).

## Decisions for the person

- **D2. Tools or Xcode profile.** Should modelled contexts use the Xcode profile, as MacPorts' builders do, or the tools profile they use now? This has been open since oracle phase 5, and changes nothing an update edits today.
- **D3. Tahoe's Xcode.** Tahoe's Xcode has no upper bound, so a `--rebuild` of its Xcode image would now choose Xcode 27, by the rule that gives Sequoia 26.3. Should Tahoe stay on 26?

### Decided

- **D4. What an update couldn't check holds a submission nobody reviews** (2026-09-27, [note](activity/2026-09-27-what-couldnt-be-checked-holds.md)).
  - `bump`'s and serve's submissions wait for a person's look, as they do for a failed search for other pull requests, when the update couldn't check something a passing build can't catch:
    - archives the comparison couldn't pair or fetch;
    - a Go toolchain minimum dockhand left below go.mod's requirement: undeclared, or one it can't rewrite.
  - Patches the update couldn't check, a Git-fetched port's, were named too. They are shown, not held: the build applies every patch and fails on one that doesn't apply, so a passing check has proven them.
  - A person's own `submit` shows these and doesn't hold.
- **D5. Tidy, rebase, and restore stay in the engine** (2026-09-27). Mechanisms they share can live in `history`, as the transitions do, and the code-organization review's `Transitions.Make` would (finding 22), but the verbs don't move there.
- **D1. Results checked under different test policies** (2026-09-27). Each result keeps the policy it was checked under, and reads under it, naming its check where that differs: "tests failed (advisory, check-3)". A later `--tests required` check of some ports doesn't bind the others, nor block the submission. The review's probe expected a block.
- **Baselines** (2026-09-27).
  - They stay optional and off by default: `--baseline`, or `check.baseline = true`.
  - A failed build, install, or test of a port the base has names the command.
  - A baseline is always dockhand's own build. MacPorts' buildbot history is never used as one, nor shown beside one, which withdraws decision 20's labeled hint.
  - Baseline results stay in the check's output. Carrying them into the pull request can come back if it's wanted.
- **Xcode images follow MacPorts' buildbots** (2026-09-27, [note](activity/2026-09-27-xcode-follows-the-buildbots.md)). A release's Xcode image has the Xcode its arm64 buildbot runs, from the facts table, as its Command Line Tools already follow the builder (decision 13). `providers.tart.xcode` in the configuration names another for a release. It replaces "the newest Xcode the release runs", which gave every release but Golden Gate a newer Xcode than MacPorts builds with.
- **A command's JSON has one shape wherever it stops** (2026-09-27, [note](activity/2026-09-27-one-json-shape.md)).
  - A command's result is a superset of what its steps and exits can say: what it didn't reach is left out or empty, and the steps it goes on to are inside it. A script then reads one shape whatever stopped the command, and the exit code says where.
  - A command that refuses before doing anything reports only its error.
  - Different commands needn't share a shape.
  - It is best effort. It replaces the same day's first ruling, that a command's JSON may follow where it stopped.
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
- **Package boundaries.** Extend import checks when touching a boundary that matters; the command boundary test, and item 4's engine boundary test, are the models. Don't split packages by size alone. Run `make deadcode` after a milestone, and keep test-only exports documented as such. It can't see an unused exported method on a type held as an interface, so sweep those by references too. CI doesn't run it.
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

**The [code-organization review](reviews/2026-09-27-code-organization-review.md)** of 2026-09-27 read `a71fc67f`. Its 46 findings were checked again at `3b16b187` ([note](activity/2026-09-27-code-organization-review-reconciled.md)).
- **Taken:**
  - before item 6: findings 1, 2, 9, 13, 17 (`ExitCode`), 18, 19 (the embedding), 20, 24, 31, 32, 33, 34, 35, 36 (the regression), 37, 38 (the unreachable path), 42, 43, and 46;
  - inside item 6: 1 (the dependencies), 4, 25, 27, 28, 36, and 39;
  - inside item 7: 6, 8, and 9 (the subject);
  - the rest as smaller items.
- **Worse than it said, through later work:** 1, 32, 35, 36, and 37. Two regressions were fixed at once.
- **Changed:**
  - 2: a misspelled `--tests` is refused; `bump` and serve don't bypass the rule; `update --submit` does.
  - 6 is P3.
  - 18: the `deadcode` advice.
  - 26: the merge loop stays.
  - 42: signal the group gently, minding a prompting `sudo`.
- **Declined:**
  - splitting `store` or `portindex` (23);
  - finding 21's plist half;
  - 41's legacy-plan deletion;
  - 20's alias and shared registry;
  - a branch shape shared across commands (10): the person's rule is one shape per command, which commands needn't share.
- **Decided by the person:** D4, that what an update couldn't check holds; D5, that the history verbs stay in the engine.

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
