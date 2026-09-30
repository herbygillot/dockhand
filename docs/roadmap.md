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

In order, with the smaller items' batches between them as [Smaller items](#smaller-items) sets out. Each item lands in its own commits with an activity note, and a review's probe becomes a regression test when its item fixes what it probes. The order is the implementer's to re-settle as work lands.

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
  - `serve --install` carries `--git`, `TART_HOME`, and the `DOCKHAND_*` settings it was installed under, never a token (finding 46). Done 2026-09-27;
  - `ExitCode` finds an exit through wrapping (finding 17). Done 2026-09-27 ([note](activity/2026-09-27-promised-rules.md));
  - every short-lived directory in the run root, as `scratch` promises: Tart's askpass helper and `submit`'s description buffer (finding 42). Done 2026-09-28 ([note](activity/2026-09-28-dead-code.md)).
- **Progress on stderr** (finding 31). Done 2026-09-28 ([note](activity/2026-09-28-progress-on-stderr.md)):
  - `-v` shows what the 48 progress reports say;
  - `outdated` prints what it found when interrupted, and clears its count.
- **The journal and serve's files:**
  - cleanup prunes old events and ended sessions (finding 34). Done 2026-09-28;
  - `check` and `watch` read the journal from where they start (finding 34). Done 2026-09-28;
  - each command opens one observer session (finding 34). Done 2026-09-28;
  - serve's and cleanup's stamps and `serving.json` are per repository, and the day's look is stamped after it (finding 35). Done 2026-09-28 ([note](activity/2026-09-28-journal-and-serve-files.md));
  - `CleanupDue` gives its reason as a type (finding 37). Done 2026-09-28.
- **Dead code.** Done 2026-09-28 ([note](activity/2026-09-28-dead-code.md)):
  - about 520 lines of `git`;
  - `commitmsg`'s unused composer and `outdated`'s unused helpers;
  - the other uncalled pieces the review and its check found (findings 18, 9, 20, 13, and 38), where a 404 now reads as a pull request not found;
  - what `deadcode` without `-test`, and a references check, found beside them.

The order puts live regressions first, then the guardrail unattended submission relies on, then small promises the code doesn't keep, then what a person watching sees, then what grows without bound, and dead code last, since removing it changes nothing.

**Before item 6 goes on: logic in the homes of the facts it interprets** (the [private-helper review](reviews/2026-09-28-private-helper-ownership.md), [note](activity/2026-09-28-private-helper-review-reconciled.md)). Done 2026-09-28. What stood on its own, in order, each in its own commits, each of its probes a regression test once fixed:
- **What a comparison couldn't read** (finding 3). Done 2026-09-28 ([note](activity/2026-09-28-source-comparison.md)): upstream source comparison is `sourcecompare`, over `archive`'s traversal. What it couldn't read of a manifest, or a file past what it reads, holds as D4 has it. Cargo's dependency tables, pyproject's arrays in either quote, Poetry's table, and go.mod are read with real parsers. The review's [follow-up](reviews/2026-09-28-private-helper-follow-up.md) found one gap left: what the old version's manifest couldn't be read for was dropped, so a gap on that side alone compared as nothing, and held nothing. Done 2026-09-29 ([note](activity/2026-09-29-old-manifest-gaps.md)): the old version's gaps hold too, named for it.
- **What `create` writes** (findings 5 and 4). Done 2026-09-28 ([note](activity/2026-09-28-what-create-writes.md)): `tcl/syntax.Quote` writes a Tcl word that reads back as its value, and one Cargo.lock reader in `macports/dependency` serves creating and updating, keeping each crate's source.
- **A stealth update's edits inside the editor** (finding 9, the open half of the earlier finding 19). Done 2026-09-28 ([note](activity/2026-09-28-stealth-in-the-editor.md)): `portedit` makes and evaluates the revision bump and `dist_subdir`, and removes the latter on a version update, given the files the branch changed since its base; edits that would change another port are left for the person.
- **The description merge's result** (finding 10). Done 2026-09-28 ([note](activity/2026-09-28-description-merge-result.md)): the merge returns each part's outcome, refreshed, current, kept, or absent, which the preview words.
- **Facts with homes** (finding 7, and the review's table). Done 2026-09-28 ([note](activity/2026-09-28-facts-with-homes.md)), each as operations in the fact's own package, its callers keeping their policies:
  - the ports tree's layout in `macports` (the earlier finding 29): `_resources`, categories, a path's port directory, Portfile paths, and PortGroups, in `macports/layout.go`, with `macports.ValidName` in the Tart archive site;
  - GitHub remote and pull request addresses in the GitHub layer (the earlier finding 13): one strict remote reader, a page reader, and the addresses of a remote and a pull request's pages, in `internal/github`;
  - provider names in `buildenv` (the earlier finding 14): constants in the contract;
  - a maintainer's identity in `macports`: reading, normalizing, and checking a maintainers line, which now refuses what Tcl reads specially;
  - which Darwin releases run on which architecture, in `macos`: `macos.RunsOn`, and Golden Gate is no longer evaluated on Intel.

The order is the roadmap's own: a guardrail first, then what's written into a Portfile, then fidelity, then structure. The facts with homes come before items 6 and 7, which would otherwise add more readers of their copies. Findings 1, 2, and 6 go inside item 6 instead, since they move the planning code and reshape the archive install that item 6 is already moving and extending; there, that code is touched once.

6. **Reuse and archives** (decisions 28 and 44; the previous step 9). This builds on item 2's predicate, and takes planning and that predicate out of the engine as it changes them (item 4).

   Taken from 2026-09-30 in this order, each in its own commits, since each later one reads what an earlier one types:
   1. the evaluator's computed facts as typed fields (finding 27, below), which eligibility and planning then read. Done 2026-09-30 ([note](activity/2026-09-30-typed-facts.md)): accessors in `macports`, a failed stub probe said, and the fetch's reason read from its assessment;
   2. build eligibility in `macports` (the private-helper review's finding 1, below). Done 2026-09-30 ([note](activity/2026-09-30-build-eligibility.md)): `macports.BuildEligibility`, with `known_fail` and `platforms` as MacPorts decides them, and an unreadable option unresolved;
   3. the conservative Portfile inspection (its finding 2, below). Done 2026-09-30 ([note](activity/2026-09-30-portfile-inspection.md)): revision-only changes, declared versions and revisions, and PortGroup references, read from the parsed source;
   4. planning out of the engine, its phases named, the port reader's evaluation report recorded, and a dependency met by a file (findings 4 and 27, below). Planning's phases, named and out of the engine, done 2026-09-30 ([note](activity/2026-09-30-planning-phases.md)): `internal/planning` decides from what the engine reads, and a plan's targets are their ports' names. A dependency met by a file closes no cycle, done 2026-09-30 ([note](activity/2026-09-30-dependency-met-by-a-file.md)). The evaluation report and the Base version on the bound probe are taken off: reuse keys on what each build recorded in the guest, so the report would have no reader, and the version's two readers hold a port, not a snapshot (the same note);
   5. results and the reuse predicate: a kind on each cell, the tests vocabulary where results are written, and an unreadable identity (findings 25, 28, and 39). Done 2026-09-30: the vocabularies and the identity ([note](activity/2026-09-30-results-as-written.md)), and a kind on each cell with one rule for an extra ([note](activity/2026-09-30-evidence-cells.md));
   6. the binary archive site out of Tart's SSH channel (the private-helper review's finding 6). Done 2026-09-30 ([note](activity/2026-09-30-binary-archive-site.md)): `macports/binaryarchive`, with the keys where they were;
   7. the JSON gaps and one set of dependencies for `engine.Open` (findings 36 and 1). Done 2026-09-30 ([note](activity/2026-09-30-json-gaps-and-composition.md)): the release outdated found goes to the update, a plan's JSON says what was asked and left out, and one GitHub client; the dependencies value is taken off, since the lock fixed the race and eager construction would cost every command.

   GitHub's reuse, and how long build history is kept (D6), are decisions for the person, taken up where the order reaches them.
   - **Per-target reuse** by recorded observations, negative ones included. Begun 2026-09-27 ([note](activity/2026-09-27-reuse-and-archives.md)): where every target an environment would build is unchanged in what it read, its earlier passed results are reused and nothing is built (`check --fresh` builds). Since 2026-09-28 ([note](activity/2026-09-28-partial-reuse.md)), the targets unchanged in what they read are reused and the rest build; the guest installs a reused one they need from its kept archive, and builds it when none is kept.
   - **What each build records:** its input identity and the digest of every archive it consumed. Done 2026-09-27 for Tart ([note](activity/2026-09-27-reuse-and-archives.md)): images keep each port's archive (setup protocol 3), and each result names its inputs by content. Those are the ports active as it built, with their archives' digests and directories, plus the target's own directory and `_resources` by tree, and the environment. It also keeps its own archive's digest.
   - **Environment identity by origin** (32). Done 2026-09-27 for Tart ([note](activity/2026-09-27-reuse-and-archives.md)): setup pins the vanilla image by digest and records each image's origin on the host. Each provider run records its environment's identity, and a result counts only while the environment is still that one. The other providers say nothing yet, so their results stand as before. Nor are they ever reused, and the plan doesn't say so: re-checking gh after a rebase reused check-35 on both Tart environments, and ran GitHub's workflow again, 5.5 of the check's 5.7 minutes, though master's one new commit touched another port (the gh rebase's finding 1). Saying so was a smaller change, done 2026-09-29 ([note](activity/2026-09-29-checks-of-the-files-as-they-are.md)). Reusing a GitHub result needs an identity for the runner's image, from the jobs API, and a record of what the workflow read, which installs MacPorts' own binaries as it runs.
   - **Archives ready for dependents** only once durably transferred and checked. Kept since 2026-09-28 ([note](activity/2026-09-28-kept-archives.md)): Tart fetches a passed target's archive, which is kept once it matches its digest, and cleanup removes those no live result names. Guests install them, for reused targets and on retries, from an archive site of MacPorts' own kind, signed with dockhand's keys. Their preparation moves out of the SSH channel before more builds on it: `macports/binaryarchive` holds the signing keys, both signatures, and a site's files, while Tart keeps the upload and the guest's configuration, and the keys stay where they are (the private-helper review's finding 6). Done 2026-09-30 ([note](activity/2026-09-30-binary-archive-site.md)).
   - **A dependency Base would find met by a file** (`bin:`, `lib:`, `path:`). Done 2026-09-30 ([note](activity/2026-09-30-dependency-met-by-a-file.md)): it still orders, and a loop it closes is no cycle; whether its file is there is the guest's to say, not the host's evaluation. It orders the targets in a plan, where Base drops it when the file is there and no port owns it: in a clean guest, `bin:git:git` is met by the Command Line Tools' git. Two targets of one branch can then be ordered, or refused as a cycle, where Base sees no dependency ([note](activity/2026-09-29-read-as-base-reads-it.md)). With the evaluation report, which would say what the environment holds.
   - **The port reader returns an evaluation report,** or a reference to its observation, rather than port names and dependency lists, so the observations can be recorded; today they would have to be reconstructed. Taken off 2026-09-30 ([note](activity/2026-09-30-dependency-met-by-a-file.md)): the review asked for it should reuse need it, and reuse keys on the inputs each build recorded, so it would have no reader.
   - **From the code-organization review,** as planning and results move:
     - `PlanCheck`'s phases named (finding 4). Done 2026-09-30 ([note](activity/2026-09-30-planning-phases.md)), with the `Validate` clause it asked for;
     - a kind on each cell of the evidence, and one rule for an `--also` extra, which status and submit read differently today (finding 25). Done 2026-09-30 ([note](activity/2026-09-30-evidence-cells.md)): serve and submit counted an extra no check built as failed, and an unmet cell filled from an earlier check lost its reason. The branch status's tree and changed paths for `diff` and its siblings stay with the finding, a saving for when those verbs are next touched;
     - the evaluator's computed facts as typed fields (finding 27). Done 2026-09-30 ([note](activity/2026-09-30-typed-facts.md)). The Base version on the bound probe is taken off, as the evaluation report is ([note](activity/2026-09-30-dependency-met-by-a-file.md));
     - the tests vocabulary checked where results are written, since reuse carries results on (finding 28). Done 2026-09-30 ([note](activity/2026-09-30-results-as-written.md)), with the phase's, and a builder's part's;
     - an identity that can't be read fails the attempt, rather than recording "no origin" (finding 39). Done 2026-09-30 ([note](activity/2026-09-30-results-as-written.md)), and `logs --json` gives the identity recorded;
     - the release `outdated` found passed to the update, and a plan's `--only`, `--also`, `--fresh`, and omissions in its JSON (finding 36). Done 2026-09-30 ([note](activity/2026-09-30-json-gaps-and-composition.md));
     - one set of dependencies given to `engine.Open` (finding 1). One GitHub client done 2026-09-30; the dependencies value taken off ([note](activity/2026-09-30-json-gaps-and-composition.md)).
   - **From the private-helper review,** as planning moves, with the evaluator's typed facts (finding 27 above):
     - build eligibility in `macports`, reading options as MacPorts does, with an unknown kept apart from an exclusion (finding 1). Done 2026-09-30 ([note](activity/2026-09-30-build-eligibility.md)). A boolean is tested in MacPorts' own interpreter, as MacPorts tests `known_fail` (`string is true -strict`), not matched against spellings in Go, which misses Tcl's `on` and its prefixes; `supported_archs` is read as a Tcl list, and an option that couldn't be read is unknown (the follow-up review). An exclusion by `platforms` is named for it: MacPorts defaults `known_fail` to yes where a port's `platforms` exclude the host, which showed as "known_fail" for beekeeper-studio, which declares none (the beekeeper-studio run's finding 3);
     - a conservative Portfile inspection in `macports/portfile`: whether a change is only to the revision, and a port's declared version, `go.setup`'s included, which tidy reads too (finding 2). Done 2026-09-30 ([note](activity/2026-09-30-portfile-inspection.md)). A revision line inside Tcl data isn't a command, and a version is read only where it's literal, so moving today's regexes isn't enough (the follow-up review). Nor is another subport's setup line the port's version, as git-devel's `github.setup` was read for git's, so tidy couldn't name a bump made by hand (the git run's finding 5).

7. **Coverage** (the previous step 13, with what `outdated` found).
   - **The 144 ports `outdated --mine` can't check,** sized by reason first. Most use Portfile conventions discovery doesn't take ([note](activity/2026-09-27-outdated-speed.md)). On 2026-09-29, `outdated --all` over the person's 896 ports couldn't check 44: 18 with custom livecheck hooks, 17 with `livecheck.type git`, 2 with no release matching the filter, 2 with no editable version input, and one each with a non-numeric version, livecheck off, a GitHub 404, an HTTPS downgrade the server redirects to (rightly refused; the Portfile's URL wants its trailing slash), and mise's dependency, which is dockhand's (a smaller item).
   - **Files that preparation adds and deletes** (40).
   - **An outcome for the 441 ports with nothing to fetch.**
   - **Literal segments of composed versions,** llvm's and openjdk's.
   - **Smaller buckets:** the Go toolchain check on gitlab.com, and the R ports' condition.
   - **Re-sizing:** the host-reader buckets after the oracle, and the PortGroup inclusion map.
   - **`create`:** from registry names (`pypi:`, `crates:`, `go:`), `--like`, and `go.vendors`. `crates:` builds on the Cargo.lock reader that creating and updating share (the private-helper review's finding 4).
   - **A `cargo.crates_github` entry's label kept,** with its commit and checksum updated, where the build resolves Git sources online (the gh, usql, hk, and pgdog run's finding 3). pgdog labels two rev-pinned crates `master`, which dockhand doesn't generate, so it refuses the whole update as a maintained override. Whether the entries are used at all when the Cargo.lock selector is `rev=` and `cargo.offline_cmd` is empty is checked first against `cargo_fetch-1.0.tcl`, which writes the label as `branch = …`.
   - **From the code-organization review,** where this work touches:
     - one livecheck pipeline for upstream's two paths, before discovery changes (finding 6);
     - `create` names `adopt` as the other authoring commands do, and records design v3's subject (findings 8 and 9).
   - **Shell completions in a created destroot,** where Cargo.lock has `clap_complete`, suggested and marked unconfirmed, as the destroot is ([create, for txt, again](reviews/2026-09-28-hugo-bump-exercise.md#create-for-txt-again), finding 5). Low priority.

8. **Variants in checks** (the libuv, sqlit-tui, ouch, and s2n-tls run's finding 7). Done 2026-09-30 ([note](activity/2026-09-30-variants-in-checks.md)), but for a baseline of a variant build, which rebuilds the port's defaults at the base. Design v3 promises `check --variants` for a single selected target, and `--variants each` evidence ticking the template's variants item; neither exists, so `submit --tested-variants` ticks a box no check can evidence. s2n-tls runs its tests only under `+tests`. It follows item 6, which keys results and reuse by what each build read, the variants asked for included; the guest, the command provider's request file, Tested on, and the evidence grid each name the variants, and GitHub's workflow, which builds only default variants, says it can't.

   Decided 2026-09-30, with the person:
   - `--variants each` builds the selected target's default variants, and then each variant it declares, one at a time over its defaults, all but `universal`, which needs other architectures' dependencies a clean guest doesn't have. Which variants a port declares is `macports`' to say (`PortInfo(variants)` and `vinfo`), and which of them `each` builds is planning's.
   - Above a number of builds, targets times environments, `check` asks first; without a terminal it needs `--yes`.
   - A passing `--variants each` check ticks the template's variants item by itself, and the description lists the variants it built.

   Already below the plan: `model.Target` carries variants, the Tart guest and the command provider's request pass them to `port install` and to the test check, and reuse keys results by them. What's to build is the plan and above. A target's identity becomes its name and its variants (the plan's `Validate` holds it to that), since `each` builds one port several times; the port reader evaluates a target with its variants, since eligibility, dependencies, Xcode, and `test.run` can change with them; a variant the port doesn't declare is refused before anything builds. GitHub's workflow builds default variants only, so `--variants` there is refused, saying so.

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

In batches, in the order they're taken, each touching one area once, so a change to an area is made once. Each item names the run that found it, whose findings are checked against the code before they're placed here (Reviews, below). Batches 1 to 3 were taken on 2026-09-29: the Go minimum, dependencies and license years as MacPorts and a reviewer read them, and what status and rebase say of checks.

The order of all the work, as of 2026-09-29:
1. batches 4 to 9, each small or medium, each putting right something a person is told or relies on today, batch 9 the most common false hold;
2. item 6, reuse and archives, with the planning it moves out of the engine;
3. batch 16, since `checksums` couldn't refresh any port with vendored crates or Go modules, and `create` couldn't finish the commonest new Rust port (added 2026-09-30). Done 2026-09-30;
4. item 8, variants in checks, which keys its results as item 6 does. Done 2026-09-30;
5. batch 17, what the helper-ownership review of 2026-09-30 reproduced: a false hold serve would stop on, an HTTPS answer that was HTTP, and the variant reader item 8 now calls (added 2026-09-30). Done 2026-09-30;
6. batch 18, a new port from create to submit without a hand-made detour, since every new port meets it (added 2026-09-30). Done 2026-09-30;
7. batches 10 to 15, with item 7's coverage beside batch 13, which reads more of the same archives, and the two boundaries that review recommends, a project's manifests read once and the update assessment in `macports`, taken before batch 13 adds more manifests.

What batches 10 to 15 touch doesn't move with item 6, so they wait without cost, and batch 12's GitHub work wants item 6's environment identity. The order is the implementer's to re-settle as work lands.

- **Batch 4: what holds, and what doesn't.** Done 2026-09-29 ([note](activity/2026-09-29-what-holds-and-what-doesnt.md)). Both directions are wrong today in the comparison that guards unattended submission: gh couldn't be compared, which holds, and a pin MacPorts can't meet passed.
  - the archives compared per context (finding 2). The current version's archives are this Mac's fetch plan's, one for gh, while the new version's are every context's, two, so the counts differ and nothing is compared. Pairing the new version's archives for the same context with the current ones would compare gh's source archive, which every modern builder fetches ([the gh, usql, hk, and pgdog run](reviews/2026-09-28-hugo-bump-exercise.md#gh-usql-hk-and-pgdog-chosen-for-untried-paths));
  - a Python requirement that excludes the version MacPorts has of a port the Portfile depends on holds. sqlit-tui 1.6.4 pins `textual-fastdatatable==0.19.0`, while MacPorts' py-textual-fastdatatable is 0.17.1, and a noarch build passes regardless. The port index at the base has the version ([the sshuttle run](reviews/2026-09-28-hugo-bump-exercise.md#sshuttle-through-paths-not-yet-taken));
  - a pin the branch's own update of the dependency meets doesn't hold: sqlit-tui 1.6.4 was taken again with py-textual-fastdatatable 0.19.0 in one branch, which the check built first ([the libuv, sqlit-tui, ouch, and s2n-tls run](reviews/2026-09-28-hugo-bump-exercise.md#libuv-someone-elses-pr-sqlit-tui-with-its-dependency-ouch-and-s2n-tls)).
- **Batch 5: checksums dockhand can't write.** Done 2026-09-29 ([note](activity/2026-09-29-checksums-dockhand-couldnt-write.md)), with a family's shared checksums, found after it. git, among the most-installed ports, can't be updated at all, and the advice given leads to the same refusal.
  - **checksums declared in a variant.** MacPorts runs a variant's body as a procedure it builds by joining strings, so Tcl records no file for its commands, and the `+doc` variant's `checksums-append` can't be located in the Portfile (`portfile.LocateDeclaration`). `distfiles.Bind` drops that reason, so the refusal says "calculated checksum algorithm", wrapped as "baseline {…}: portfile: unsupported source edit: …", which the command can't trim, and its advice, `dockhand checksums git`, meets the same refusal (findings 1 and 2). git's computed names work, as yq's did. Finding a declaration a variant runs in the variant's body is `macports/portfile`'s; the refusal's reason is `distfiles`' to keep, typed, for the command to word, with advice only where it can work ([the git run](reviews/2026-09-28-hugo-bump-exercise.md#git-a-port-dockhand-couldnt-update));
  - `checksums` printing, where it can't write them, the lines for every distfile of the default variants, from MacPorts' fetch plan, which `portedit/archives` can compute (finding 3) ([the git run](reviews/2026-09-28-hugo-bump-exercise.md#git-a-port-dockhand-couldnt-update));
  - no "Kept: the branch, unchanged." from a plan made on master, which starts no branch (finding 4) ([the git run](reviews/2026-09-28-hugo-bump-exercise.md#git-a-port-dockhand-couldnt-update)).
  - a checksum refresh of a port whose family shares one declaration (finding 1). `checksums py-flatbuffers` was refused as an unintended change to the stub's checksums, which it shares with its subports: an unscoped refresh let only the selected port's change. Done 2026-09-29 (the same note): a sibling of the same version whose checksums were the selected port's, and are its new ones, moves with it ([adding py-flatbuffers](reviews/2026-09-28-hugo-bump-exercise.md#adding-py-flatbuffers-to-35044)).
- **Batch 6: said before acting.** Done 2026-09-29 ([note](activity/2026-09-29-said-before-acting.md)). What a person needs to hear before a step they can't easily take back.
  - `update --plan` naming other open pull requests for the port (finding 1). It planned libuv 1.53.0 without a word of #34620; only submit's preview looks (`engine/submit.go`, the one caller of `OpenPullRequests`), and `bump` holds on it, after the work ([the libuv, sqlit-tui, ouch, and s2n-tls run](reviews/2026-09-28-hugo-bump-exercise.md#libuv-someone-elses-pr-sqlit-tui-with-its-dependency-ouch-and-s2n-tls));
  - `tidy --group` warning where it puts a port before one it depends on (finding 6). `--group "2 1"` would have committed sqlit-tui, which pins textual-fastdatatable 0.19.0, before the commit providing it. `Regroup` reads only the numbers; the check of the files has the plan's order ([the libuv, sqlit-tui, ouch, and s2n-tls run](reviews/2026-09-28-hugo-bump-exercise.md#libuv-someone-elses-pr-sqlit-tui-with-its-dependency-ouch-and-s2n-tls));
  - a submission waiting on a queued or running check of the same files names it and `dockhand wait <check>`, rather than "run dockhand check first" ([the sshuttle run](reviews/2026-09-28-hugo-bump-exercise.md#sshuttle-through-paths-not-yet-taken));
  - a database migration that says so, since builds older than it can't open the database afterward; certigo's first run migrated it silently ([the certigo run](reviews/2026-09-28-hugo-bump-exercise.md#certigo-with-a-hands-on-binary-test)).
- **Batch 7: baselines and test evidence.** Done 2026-09-29 ([note](activity/2026-09-29-baselines-and-test-evidence.md)). A baseline is how a person tells a branch's failure from master's, and tests are what it was run for here.
  - `--baseline` reading advisory test results (finding 5). `BaselineWorthy` takes only failed targets, so after check-38's advisory test failures it said "nothing failed … name ports with --only"; with `--only`, `baselineWords` compares outcomes, which advisory test failures don't change, so the summary said "✓ builds at the base, as it does on the branch" of uvw, whose tests fail at the base too ([the libuv, sqlit-tui, ouch, and s2n-tls run](reviews/2026-09-28-hugo-bump-exercise.md#libuv-someone-elses-pr-sqlit-tui-with-its-dependency-ouch-and-s2n-tls));
  - `check --baseline --plan` previewing the baseline, not refused with cobra's raw "if any flags in the group [baseline plan] are set none of the others can be" (finding 4); `MarkFlagsMutuallyExclusive("baseline", "plan")` in `command/check.go` ([the libuv, sqlit-tui, ouch, and s2n-tls run](reviews/2026-09-28-hugo-bump-exercise.md#libuv-someone-elses-pr-sqlit-tui-with-its-dependency-ouch-and-s2n-tls));
  - `--tests required` saying where a port declares no tests (finding 3). Such a port passes under any policy, as designed, but the plan says "tests required" and the grid "✓" for it as for tests that passed. Only the guest reads `test.run`; the plan's words would need `macports` to read it ([the ov run](reviews/2026-09-28-hugo-bump-exercise.md#ov-through-adopt-edit-retry-and-submit---passing)).
- **Batch 8: what the previews and summaries say.** Done 2026-09-29 ([note](activity/2026-09-29-what-previews-say.md)). Small, and read on every submission.
  - `submit --plan` showing the pull request's description, which both exercises read through `--json` ([the hugo exercise](reviews/2026-09-28-hugo-bump-exercise.md));
  - `submit --passing`'s upstream lines under a label of their own, and so without "upstream:", as the single preview's are (finding 5). The prefix was kept on purpose, since those lines had no heading (4315d6bd) ([the ov run](reviews/2026-09-28-hugo-bump-exercise.md#ov-through-adopt-edit-retry-and-submit---passing));
  - `submit --ready --plan` previews marking ready, and a draft's preview names `dockhand submit --ready` ([the sshuttle run](reviews/2026-09-28-hugo-bump-exercise.md#sshuttle-through-paths-not-yet-taken));
  - the update's summary counting the crates and Go modules it wrote, not only distfiles (finding 4): hk's said "1 distfile" of a change that rewrote 280 `cargo.crates` lines. `Distfiles` counts archives and Git crates only ([the gh, usql, hk, and pgdog run](reviews/2026-09-28-hugo-bump-exercise.md#gh-usql-hk-and-pgdog-chosen-for-untried-paths));
  - a legend for the preview's `!`, which marks what holds the branch for a look ([the chezmoi run](reviews/2026-09-28-hugo-bump-exercise.md#chezmoi-with-bump));
  - the stub notice said once: the editor's `load` reports it each time an update loads the port, as its probe and its preparation do, so py-pipdeptree's update said it three times ([the sshuttle run](reviews/2026-09-28-hugo-bump-exercise.md#sshuttle-through-paths-not-yet-taken));
  - serve's banner says "1 check at a time": one Tart check still builds two releases at once ([the sshuttle run](reviews/2026-09-28-hugo-bump-exercise.md#sshuttle-through-paths-not-yet-taken));
  - a PyPI release with no sdist said as such (finding 2): `update py-flatbuffers 25.12.19` stopped with a 404 and "no archive is published at that location yet", but PyPI has only a wheel for it, and never will have an sdist. PyPI's JSON API, a documented interface, says which files a release has, so the refusal can say "PyPI publishes only a wheel" and name the forge's release as the source to use ([adding py-flatbuffers](reviews/2026-09-28-hugo-bump-exercise.md#adding-py-flatbuffers-to-35044)).
- **Batch 9: holds scoped to what the build reads.** Done 2026-09-30 ([note](activity/2026-09-30-holds-scoped-to-the-build.md)), but for the first item, chezmoi's, whose manifests of an unused build system are set apart now, while which manifests below the top level the build uses joins batch 13's yarn workspaces. With the copyright years gone, holds on files the port's build never reads are the most common false stop of an unattended submission.
  - a comparison that knows which manifests the port builds with, and sets the rest apart: chezmoi's pyproject.toml is its documentation's. It reads those below the top level the build uses, too, which it doesn't yet: beekeeper-studio's yarn workspace `apps/studio/package.json` added two dependencies and moved electron from 39.8.5 to 39.8.10, unseen, as a Node version in `.nvmrc` or the lockfile would be (the beekeeper-studio run's finding 1) ([the chezmoi run](reviews/2026-09-28-hugo-bump-exercise.md#chezmoi-with-bump));
  - holds on the files of a build system the port doesn't use (finding 2): flatbuffers, a C++ port built with CMake, held on `package.json`, eslint's, and on `Package.swift`. Which build systems a port uses is MacPorts' to say, by the PortGroups it loads and its configure and build commands, and only their files, and their dependency manifests, should hold ([the flatbuffers, nuspell, zola, and alertmanager run](reviews/2026-09-28-hugo-bump-exercise.md#flatbuffers-nuspell-zola-and-alertmanager));
  - a build file whose change is only the version it names doesn't hold, as a license whose change is only its copyright years doesn't (finding 2): nuspell's `CMakeLists.txt` changed only `project(... VERSION 5.1.9)`. How much further to read a build file, such as CMake's packaging directives, since flatbuffers' change was to source lists and tests, is D12 ([the flatbuffers, nuspell, zola, and alertmanager run](reviews/2026-09-28-hugo-bump-exercise.md#flatbuffers-nuspell-zola-and-alertmanager)).
- **Batch 10: branches, done, never dockhand's, or rewritten more than they need.** What status, clean, and adopt don't yet see, and what tidy rewrites that it could keep.
  - status saying that a branch's changes are already on master (finding 1). It compares a branch only with its recorded base, so duckdb-cxx14's edit, landed on master by another route, still asked "commit it for review". With the patch-id comparison "Branches from before v3" plans, below, for branches dockhand doesn't track ([cleaning up duckdb-cxx14](reviews/2026-09-28-hugo-bump-exercise.md#cleaning-up-duckdb-cxx14));
  - an archived branch's Git branch, with nothing master lacks, going with its worktree (finding 2). clean keeps an archived branch's Git branch on purpose, since its work isn't merged. One with nothing beyond master could go too, once status and checking out again treat it as a merged branch's: today status would call it gone, and `checkOutAgain` would fail ([cleaning up duckdb-cxx14](reviews/2026-09-28-hugo-bump-exercise.md#cleaning-up-duckdb-cxx14));
  - branches from before v3 ([the cleanup](reviews/2026-09-28-hugo-bump-exercise.md#cleaning-up), finding 3): the hugo exercise's checkout holds 22 local `dockhand/bump/<port>-<id>` branches from earlier dockhand, which nothing reports. Its classification is the design:
    - in master by patch-id (`git cherry`, since MacPorts rebases on merge), as 13 were, whatever became of their pull requests: removable, with the fork branch when it holds the same commit;
    - superseded by a newer version in master, as 1 was: removable after a look;
    - unfinished, as 8 were: listed with `adopt` to take one up, never removed. `clean` could list them, and remove the first kind with its fork branches when asked;
  - an adopted branch that isn't checked out given a worktree, as `start` makes one (finding 3). `adopt` and `path` advise `git switch`, which switches the person's own checkout, and a worktree the person adds later isn't found: a tracked branch's worktree is recorded only as it's adopted (`Branch.Worktree`) ([the flatbuffers, nuspell, zola, and alertmanager run](reviews/2026-09-28-hugo-bump-exercise.md#flatbuffers-nuspell-zola-and-alertmanager));
  - a branch from before v3 whose commits carry an old `Generated-By: Dockhand …`, said where rebase keeps it and where tidy replaces it (finding 4) ([the flatbuffers, nuspell, zola, and alertmanager run](reviews/2026-09-28-hugo-bump-exercise.md#flatbuffers-nuspell-zola-and-alertmanager));
  - tidy keeping the commits it wouldn't change (finding 3): `tidy --apply` re-created #35044's first two commits with the same trees and messages (41a350c to a44172c, 4915236 to 4da0ee1), since it writes every commit anew, with the committer's time now, so the re-submit replaced the pull request's history instead of pushing one commit more. Where a proposed commit is the branch's own, with the same parent, tree, message, and author, it can stay ([adding py-flatbuffers](reviews/2026-09-28-hugo-bump-exercise.md#adding-py-flatbuffers-to-35044));
  - an `edit` of another port narrowed away again, in a worktree dockhand made, once nothing of the branch's is in it (finding 6). An adopted worktree's sparse set is the person's to keep ([the ov run](reviews/2026-09-28-hugo-bump-exercise.md#ov-through-adopt-edit-retry-and-submit---passing)).
- **Batch 11: someone else's pull request, and a port's dependents.** `review` does less than Design v3 §6.11 says, and what the index says of dependents is only the default variants'.
  - `review` giving what `update` finds (finding 2): the patch check, the upstream comparison, and the dependents. It applies the commit and Portfile rules only (`engine/review.go`), so it passed #34620 without the patch check that `update libuv --plan` failed in five files, or the two patchfiles the pull request comments out. The design also says it lints and that `--check` builds the pull request; it does neither, and says "not checked here: port lint and the build" ([the libuv, sqlit-tui, ouch, and s2n-tls run](reviews/2026-09-28-hugo-bump-exercise.md#libuv-someone-elses-pr-sqlit-tui-with-its-dependency-ouch-and-s2n-tls));
  - `impact`'s next step choosing dependents by something other than the alphabet (finding 3): it suggests the first three, `aria2,bind9,bind9.18`, among the heaviest to build. With nothing to size a build by, it could name how many there are and leave the choice, or take one of each dependency kind ([the libuv, sqlit-tui, ouch, and s2n-tls run](reviews/2026-09-28-hugo-bump-exercise.md#libuv-someone-elses-pr-sqlit-tui-with-its-dependency-ouch-and-s2n-tls));
  - dependents that link the port only under a variant, listed apart, as optional, for `--revbump-dependents` and `impact`, with `--except` taking them (finding 1). enchant2 links nuspell only under `+nuspell`, and nuspell's Portfile asks for enchant2's revision bump; the index at the base records default variants' dependencies only, so dockhand said "none" and refused `--except enchant2` ([the flatbuffers, nuspell, zola, and alertmanager run](reviews/2026-09-28-hugo-bump-exercise.md#flatbuffers-nuspell-zola-and-alertmanager)).
- **Batch 12: the GitHub provider.** With item 6's environment identity, which GitHub's reuse needs.
  - the github provider's `dockhand-check/` branch removed from the fork when its run finishes; clean removes them only once the branch is merged ([the sshuttle run](reviews/2026-09-28-hugo-bump-exercise.md#sshuttle-through-paths-not-yet-taken));
  - the GitHub environment's macOS release in Tested on, from the jobs API's runner labels. Its Xcode is only in the log's text, which isn't a documented interface ([the sshuttle run](reviews/2026-09-28-hugo-bump-exercise.md#sshuttle-through-paths-not-yet-taken)).
- **Batch 13: the comparison's depth.** What the upstream comparison doesn't yet read, beside item 7's coverage.
  - a yarn workspace's manifests read (finding 1): with the chezmoi run's item on which manifests the port builds with, above ([the beekeeper-studio run](reviews/2026-09-28-hugo-bump-exercise.md#beekeeper-studio-a-port-that-needed-a-portfile-change));
  - a Python port's own line, as a Go port's `go` directive has one: `requires-python` moving past the version the port pins. sshuttle 2.0.0 raised its floor to 3.10 ([the sshuttle run](reviews/2026-09-28-hugo-bump-exercise.md#sshuttle-through-paths-not-yet-taken));
  - a quiet note, not a hold, where a port pins an older Python than the PortGroup's default, as sshuttle pins 3.13 against 3.14 ([the sshuttle run](reviews/2026-09-28-hugo-bump-exercise.md#sshuttle-through-paths-not-yet-taken));
  - an upstream comparison for a version changed by hand, from `check` or `submit`, which today say nothing of one (finding 6) ([the git run](reviews/2026-09-28-hugo-bump-exercise.md#git-a-port-dockhand-couldnt-update)).
- **Batch 14: speed, and finding one's way in a log.**
  - `logs --port` starting where the port's own phases do, or listing them with their lines: hugo's began near line 46,400 of 47,000, after its dependencies' ([the hugo exercise](reviews/2026-09-28-hugo-bump-exercise.md));
  - `outdated` for one named port, which took 12 seconds in both ([the hugo exercise](reviews/2026-09-28-hugo-bump-exercise.md));
  - the index a check stages found nearer to hand (finding 4). check-23 built a whole index for macOS 15, which no earlier check at that master had used, so the review's premise was another release's. Still, a Tart check's stager builds no base index, seeds only from the same release's recent generations, and never from the mirror for a snapshot, which has no commit; `portindex/source.go` says verification keeps the base, which the wiring doesn't ([the ov run](reviews/2026-09-28-hugo-bump-exercise.md#ov-through-adopt-edit-retry-and-submit---passing)).
- **Batch 15: provenance.**
  - a `Generated-By` naming a commit nobody can find, not only a `+dirty` build: submit could ask GitHub, since tidy reads nothing remote ([the hugo exercise](reviews/2026-09-28-hugo-bump-exercise.md)).
- **Batch 16: a new Rust port, and checksums of vendored sources** ([create, for txt](reviews/2026-09-28-hugo-bump-exercise.md#create-for-txt)). Done 2026-09-30 ([note](activity/2026-09-30-a-new-rust-port.md)), with a Cargo or Go port's checksums naming their file, which lint found as it ran. Taken after item 6, before the rest: the first item stops `checksums` for every port with `cargo.crates`, `go.vendors`, or `cargo.crates_github`, not only a new one.
  - `checksums` of a port with vendored sources (finding 1). `create` runs the checksum refresh, which binds the Portfile's archives as written, and `archives.CheckPolicy` refuses a port whose `cargo.crates` is set: "fetch customization or vendored source requires a dedicated preparer". txt was left with zeros, and `dockhand checksums txt`, the advice given, meets the same refusal. `update` has the step it lacks: it sets the dependency blocks aside (`plan.Strip`), computes the source archive's checksums, and puts them back (`plan.Apply`). The refresh should do the same in `portedit`, the crates' checksums being Cargo.lock's already;
  - a destroot for a Cargo or Go port, marked unconfirmed (finding 2). `newport` writes none for any build system, and neither PortGroup installs anything, while its unconfirmed list names the build for Go and Python only. For one binary named for the package, the `xinstall` line the peer gives, and the build among what's unconfirmed;
  - the license and description from the manifest before the forge (findings 3 and 5). GitHub said NOASSERTION, so `license unknown`, where Cargo.toml says `MIT OR Apache-2.0`, MacPorts' `{MIT Apache-2}`; `newport.License` maps a single SPDX ID only. Cargo.toml's `description` is shorter and nearer MacPorts' style than GitHub's. An SPDX expression, `OR` as a choice and `AND` as both, joins `newport`'s map. MacPorts' names follow the Guide's `license` rules (`portfile-keywords.xml`): the name, a hyphen, and the version with any `.0` dropped; `+` for "or later"; licenses separated by spaces all apply, and a braced sub-list is a choice of one. Base's lint (`portlint_run.tcl`) errors on a name ending in a digit before the version's hyphen, and on `BSD-2`, `BSD-3`, and `BSD-4`. The wiki's [license keyword list](https://trac.macports.org/wiki/PortfileRecipes#licensekeyword) is the values in use as of 2020, not a list of valid ones, so the tree's use is the check. By it, the map has one wrong entry: `CC0-1.0` becomes `CC0-1`, which lint refuses, and which no port uses, while 446 use `public-domain`, which `CC0-1.0` and `Unlicense` should map to (2026-09-30). The map is `macports`' to own once a manifest's license reads it too, a second consumer, as the private-helper review allowed;
  - the category's guess said with its value (finding 4). It's `python` or `devel` by build system, marked in the file, and in the output only as the word "category" among what's unconfirmed; since it picks the directory, `create` should print what it chose;
  - HTTP URLs said, since MacPorts prefers HTTPS (finding 6, widened at the person's word, 2026-09-30). `create` wrote txt's `http://` homepage from GitHub's metadata as given, where https answers. Wherever dockhand writes or reads a Portfile's URLs, an `http://` homepage, or a `master_sites` or distfile URL the Portfile names literally, is said to the person: by `create` for what it writes, and by `update` and `checksums` for the port they edit, with whether the https form answers. `create` writes the https form where it answers; an existing Portfile's URLs are only mentioned, since changing them is the maintainer's call and no part of the update. Mirror groups MacPorts expands are MacPorts' own, and aren't mentioned. Which URLs a Portfile declares, and which the fetch plan uses, are `macports`' to say (`portedit/archives`, `distfiles`); whether https answers is a probe the engine makes, as `create`'s other reads are. `port lint` checks no scheme, so this is dockhand's own notice, not a lint finding.
- **Batch 17: what the helper-ownership review reproduced** ([review](reviews/2026-09-30-helper-ownership.md)). Done 2026-09-30 ([note](activity/2026-09-30-helper-review-fixes.md)). Each of its seven probes still fails at `85df763e`, and each becomes a regression test.
  - a requirement's condition kept, and read for macOS (finding 1). The Python reader stops at `;`, so an environment marker is dropped: a Windows-only pin moving past MacPorts' version holds an update serve would otherwise submit, and a requirement becoming Darwin's changes nothing. PEP 508's markers are evaluated as the port's own build would see them (`sys_platform`, `platform_system`, `os_name`, and the Python version the port uses); one that can't be read is unknown, and holds as D4 has it. A second declaration of a name under another condition is kept, not overwritten. A Cargo dependency's Git source and revision are compared with its version, and their change is said, holding nothing, as D9 has it;
  - an HTTPS answer that is one (finding 2). The probe follows a redirect to plain HTTP and counts it, where `fetch.Open` refuses the same downgrade, so `create` could write an `https://` homepage that answers over HTTP. The probe moves to `fetch`, with its redirect rule, and says the final URL; its success is its own, any answer below 400, not `Open`'s 200;
  - a Portfile's script bodies told from its data (finding 3). `ArchiveVariants` descends into every braced word, so a variant named inside a string, or a `checksums` inside a variant's `set`, is taken for one that fetches archives, and asked about. One traversal in `portfile` distinguishes a body that runs from data, and inspection, archive variants, and version candidates each choose their descent; `subportBody` and `BumpRevision` share one way to find a subport's block;
  - the variant reader's failures kept (finding 4). `Variants` reads `variants` and `vinfo` without their evaluation errors, and drops a malformed `requires` or `conflicts`. Item 8's planning now calls it, so a port whose variants couldn't be read plans as declaring none. Checked option reading inside `macports`, value, absence, or failure, with list and dictionary errors kept, which the other accessors share;
  - a category `create` can write (the review's table). `create --category _resources` passes its check, which refuses only a slash or a space, while `macports.IsCategory` refuses a directory beginning `_` or `.`. A category validator in `macports`, used before anything is written;
  - one owner of which results a baseline rebuilds: `rebuildWhere` and `BaselineWorthy` repeat the install, test, and advisory-failure predicate;
  - the prepared port read one way: `describe` and `preparedPort` read the editor's fidelity history and `Unchanged`, while the comparison reads `Prepared`. An accessor on the editor's result.
- **Batch 18: a new port, from create to submit** ([create, for txt, again](reviews/2026-09-28-hugo-bump-exercise.md#create-for-txt-again)). Done 2026-09-30 ([note](activity/2026-09-30-a-new-port-end-to-end.md)). txt passed check-50 once the person had moved it, re-created it, and spelled tidy's subject out; each detour is one of these.
  - a created port moved to another category (finding 1). Without a terminal the category is the build system's guess, `devel`, and nothing moves a port afterwards: `git mv` is a no-op in the sparse worktree, and `create --category editors` in the same branch refused with "there is already a port txt … dockhand update txt updates it", wrong for a port create itself wrote, uncommitted, with nothing to update to. `create` again, for the port this branch created and hasn't committed, moves it to the category named, keeping the person's edits, and `refuseExisting` says so rather than advising `update`. The guess itself can read the project's description and topics against the tree's category names (an "editor" is `editors`), still marked unconfirmed;
  - `check --branch` on a branch with no commits taking its working files (finding 2). Asking head or working tree is the design's for a branch checked out elsewhere with edits, but with no commits its head is its base, which holds nothing of it, so the working files are the only answer;
  - a created port's hand edits leaving tidy's plan unambiguous (finding 3). tidy proposed "txt: new port", then wanted `--squash --message` with the same words, since the file "has changes dockhand's commands did not make". Editing a created port is what `create` asks for ("Next: dockhand edit txt"), so for a port this branch created, the edits are the port's, and the plan stands; an update edited by hand still asks;
  - a new port's pull request saying what the port is (finding 4): its description, homepage, and license, from the Portfile as evaluated, under Description, where the commit has no body. Its Type(s) stay unticked, since MacPorts' template says a new Portfile, a "submission", is detected and labelled by its own automation;
  - the update's summary without its zero clauses, and the upstream count naming what moved (finding 6): "and 0 Git crates (0 changed)" is said only where there are any; "Cargo.toml: 1 added, 1 moved" names them, inferno added and `open` moved, where there are few, and a dependency turning from optional to required is a move, which `cargoRequirement` reads today as no change.

### Taken when their area is next touched

- **Tart workarounds to retire as Tart releases fixes:**
  - the retry of a listing that raced a delete ([openai/tart#1353](https://github.com/openai/tart/issues/1353));
  - trusting a delete only by the VM's absence (#1345, fixed by #1350 on 2026-09-26, hours after 2.39.0 was tagged: unreleased as of 2026-09-27);
  - guests reached over SSH, never `tart exec` (#1346); setup's agent readiness probe is the last `tart exec`, and could move to SSH.
- **The code-organization review's smaller findings,** each when its files are next touched. Findings are the [review](reviews/2026-09-27-code-organization-review.md)'s, as the [note](activity/2026-09-27-code-organization-review-reconciled.md) corrects them.
  - **Tart:** one SSH wait that stops at a refused login, and a refusal the runner doesn't retry: about twelve minutes today (finding 7). The facts tool's wait is a third copy (the private-helper review's finding 8).
  - **Serve:** its workers each say a problem once, and its daily look runs off the loop (finding 3).
  - **Copies to fold:**
    - submit's phases, keeping `--accept`'s errors (finding 5);
    - run recipes (finding 11);
    - tidy's rules adapter (finding 12);
    - environment words (finding 14; its provider names were done with the private-helper item);
    - one table test for exec admission (finding 15);
    - `forge/github`'s guards (finding 16);
    - small helpers (finding 17);
    - the one-shots that call `os/exec` where `subprocess.Run` would bound their wait and name their failure: `security`, `pkgutil`, `gh auth token`, `open`, `osascript`, `launchctl`, and `port version` (finding 42);
    - a pull request's head (finding 38).
  - **JSON:**
    - status's branch embeds the reference other commands give (finding 10).
  - **Structure:**
    - tidy's message composer in `commitmsg`, beside the rules it uses (finding 9);
    - `macports`' program and platform mechanics in a subpackage (finding 21);
    - the history transition as one `Make`, keeping submit's merge check (finding 26);
    - `Update`'s and `PlanTidy`'s seams (finding 44).
  - **Latent:**
    - an edit record whose commit was uncertain is read back, as a new branch's is: `Update` and `Create` write their files first, and holds are read from the records (finding 24);
    - `ls-remote` in a fresh scratch directory (finding 30);
    - the index cache's identity off a Mac (finding 40). Done 2026-09-29 ([note](activity/2026-09-29-modelled-with-xcode.md)), with D2: a cache is kept for what its indexer is told;
    - store error kinds documented (finding 41);
    - anonymous GitHub remembered for a few minutes (finding 45).
- **The observer's boundary at Golden Gate.** A boundary at `${os.major} >= 27` samples nothing below it, since its lower neighbor, Darwin 26, never shipped; the release below, 25, should stand in ([note](activity/2026-09-28-facts-with-homes.md)).
- **A flake to watch.** `TestTidyAsksWhatItCannotKnow` once failed in its cleanup with a directory not empty ([note](activity/2026-09-27-stopped-checks.md#seen-once-not-explained)).

### Done, by the run that asked

What each exercise run asked for that is done, or that joined a numbered item, with its note.

- **What updating real ports asked for,** from the duckdb and hugo exercises of 2026-09-28 ([duckdb](activity/2026-09-28-duckdb.md), [hugo](reviews/2026-09-28-hugo-bump-exercise.md)):
  - a Go port's own line: the `go` directive upstream's go.mod gives, and that `go.toolchain_min` still holds, said when it does too, since silence reads the same as not having looked. A minimum dockhand raised was said only as the update ran, so the submit preview and `--passing` never showed it (the ov run's finding 1, and yq's 4). Done 2026-09-29 ([note](activity/2026-09-29-go-minimum-as-go-mod-writes-it.md)): each outcome is an upstream line, and only a minimum left unmet holds. The pull request shows it in its diff, and its description carries no upstream finding, this one included;
- **What the chezmoi run asked for** ([review](reviews/2026-09-28-hugo-bump-exercise.md#chezmoi-with-bump)):
  - a Go module the build already required indirectly is no addition when it becomes direct. Done 2026-09-28 ([note](activity/2026-09-28-go-module-promotion.md)): go.mod's indirect requirements are read apart, and a module moving between them and the direct ones is said only where its version moves, without holding;
- **What the sshuttle run asked for** ([review](reviews/2026-09-28-hugo-bump-exercise.md#sshuttle-through-paths-not-yet-taken)):
  - **the current version's archives found as the new version's are.** Done 2026-09-28 ([note](activity/2026-09-28-mirror-groups-and-ready.md)): from MacPorts' own fetch plan, mirror groups expanded, for updates and `diff --archive`. About a third of the tree, the ports on PyPI's, CPAN's, and other mirror groups, can be compared, and so submitted unattended;
  - `submit --ready` refused by an organization's OAuth App access restrictions says what to do. Done 2026-09-28 (the same note): its page, or `gh pr ready <n>`, whose app signs in on its own. Whether dockhand should use the GitHub CLI's login for that one call is the person's (D8);
  - `wait` with no argument follows the check of the branch checked out here, as bare `logs` does. Done 2026-09-28, with the certigo run's `cancel` ([note](activity/2026-09-28-certigo-fixes.md));
- **What re-submitting sshuttle asked for** ([review](reviews/2026-09-28-hugo-bump-exercise.md#moving-sshuttle-to-python-314-and-re-submitting)):
  - a saved tidy plan whose messages read as plain text. Done 2026-09-28 ([note](activity/2026-09-28-re-submitting.md)): TOML, each message a multi-line literal string, as the guide's `plan.toml` always said; plans saved as JSON before are still read;
  - `tidy --apply` showing each commit's whole message as it will be written. Done 2026-09-28 (the same note): without the saved notes on how the proposal was made, and a subject's source says "dockhand's commit" of one carrying dockhand's Generated-By line;
  - a re-submit refreshing the Description while it's still exactly as dockhand last wrote it, as it refreshes the Type(s). Done 2026-09-28 ([note](activity/2026-09-28-re-submitting.md)): a commit's body written after the pull request opened reaches it, and the preview says so.
- **What the certigo run asked for** ([review](reviews/2026-09-28-hugo-bump-exercise.md#certigo-with-a-hands-on-binary-test)):
  - a subject kept where a person edits on top of dockhand's uncommitted update. Done 2026-09-28 ([note](activity/2026-09-28-certigo-fixes.md)): while every line dockhand's edits wrote still stands, their subject does, noted. Reading a declared version, `go.setup`'s included, stays with the private-helper review's finding 2, inside item 6;
  - `update --json`'s upstream messages without the "upstream: " prefix that the text drops under its heading. Done 2026-09-28 (the same note);
  - `cancel` with no argument, like `wait`: the check of the branch checked out here, not cobra's error. Done 2026-09-28 (the same note), for both;
  - a check cancelled while only queued said so, not "what it finished is kept". Done 2026-09-28 (the same note);
  - `status --json`'s serve as fields, whether it runs, its pid, and the queue, beside the sentence. Done 2026-09-29 (the same note): `serve_state`, in `queue --json` too, read by the engine;
  - progress reported while a run is driven journaled as the run's progress events, as a provider's already is. The PortIndex rebuild, five minutes of check-16, showed only in serve's log, and `wait` never saw it. Done 2026-09-29 (the same note): each environment's info reports, named for it;
- **What the beekeeper-studio run asked for** ([review](reviews/2026-09-28-hugo-bump-exercise.md#beekeeper-studio-a-port-that-needed-a-portfile-change)):
  - **an environment the check excluded isn't called tested,** the run's most serious finding (finding 4). `platforms {darwin >= 23}` excluded macOS 12, and the grid said so. But `submit --plan` said the check passed there, and the pull request's Tested on listed it, "its version not recorded", though nothing built there. Tested on lists every planned environment (`engine/body.go`), and submit's plan joins them itself (`command/submit.go`). Which environments a check tested is the evidence's to say, and an excluded one is named as excluded, with why. Done 2026-09-29 ([note](activity/2026-09-29-excluded-not-tested.md)): Tested on names only where something was built or reused, and submit's plan adds where every port is excluded;
  - a failure summarized by its cause (finding 2). The guest takes MacPorts' last three `Error:` lines: "`make` failed with exit code: 2; Failed to build beekeeper-studio". The cause, "fatal error: 'source_location' file not found" as node-gyp rebuilt sqlanywhere, was only in the log. A build tool's output isn't MacPorts' interface, so what's read beyond MacPorts' own lines, and how it's marked as a reading of the log, was decided first. Done 2026-09-29, as D10 decided ([note](activity/2026-09-29-a-failures-likely-cause.md));
  - an exclusion by `platforms` named for it, not "known_fail" (finding 3): with build eligibility, in item 6. Done 2026-09-30 ([note](activity/2026-09-30-build-eligibility.md));
- **What the decisions of 2026-09-29 ask for,** after the beekeeper-studio run's first item:
  - D9's holds: none for Go; a new `-sys` crate listed with a hint at the MacPorts library it may link; counts for the rest. Done 2026-09-29 ([note](activity/2026-09-29-go-and-rust-hold-nothing.md)), for Rust too;
  - D7's plan rule. Done 2026-09-29 ([note](activity/2026-09-29-plans-beside-untracked-branches.md));
  - D8's `gh pr ready`, and the guide on asking an organization to approve dockhand's app. Done 2026-09-29 ([note](activity/2026-09-29-ready-through-the-github-cli.md));
  - D10's first compiler error, in a package of its own. Done 2026-09-29 ([note](activity/2026-09-29-a-failures-likely-cause.md)), in `buildlog`;
  - D2's default, with the oracle's design note. Done 2026-09-29 ([note](activity/2026-09-29-modelled-with-xcode.md)).
- **What cleaning up after beekeeper-studio and ov asked for** ([review](reviews/2026-09-28-hugo-bump-exercise.md#cleaning-up-after-beekeeper-studio-and-ov)):
  - **a branch checked out where clean leaves it isn't deleted.** clean removed an adopted branch while the person's own worktree had it checked out, which it rightly left in place, so the worktree stood on a branch that was gone. Done 2026-09-29 ([note](activity/2026-09-29-clean-keeps-checked-out-branches.md)): a branch checked out anywhere but the worktree clean removes is kept, and says where, looked for when planned and again before it goes.
  - **`status` changes nothing.** `status --all` checked an archived branch's worktree out again after `clean --archived` removed it, undoing clean, then asked for it to be checked again. Done 2026-09-29 ([note](activity/2026-09-29-status-reads-only.md)): status reads a worktree only where it's there, and an archived branch's checks ask nothing of the person.
- **What the yq run asked for** ([review](reviews/2026-09-28-hugo-bump-exercise.md#yq-where-dockhand-removed-a-line-the-port-needed)):
  - **a `dist_subdir` removed only where every distfile's name changes with the version** (finding 1). Every version update removes a top-level `dist_subdir ${name}/${version}_${revision}`, or `_N`, the forms MacPorts' guide gives a stealth update, without looking at the distfiles (`portedit`'s `dropStealthDistSubdir`). yq's man page keeps one name across versions, which the line keeps apart on the mirrors. Without it, check-27 failed at checksum on Tart, while GitHub, fetching from GitHub, passed, so a check on GitHub alone would have let it be submitted. The removal is `portedit`'s, which has both versions' fetch plans: it keeps the line where any distfile's name is the same in both. Done 2026-09-29 ([note](activity/2026-09-29-a-dist-subdir-still-needed.md));
  - the removal said in `update --plan`'s text, as the update says it (finding 2). The plan shows only the diff's `-` line. Done 2026-09-29 (the same note).
- **What the git run asked for** ([review](reviews/2026-09-28-hugo-bump-exercise.md#git-a-port-dockhand-couldnt-update)):
  - A hand-made version bump that tidy couldn't name (finding 5) is item 6's Portfile inspection, done 2026-09-30 ([note](activity/2026-09-30-portfile-inspection.md)): the version is read from the first forge setup line anywhere, here git-devel's `github.setup` in its subport, so git's own `version` line was never read.
- **What the ov run asked for** ([review](reviews/2026-09-28-hugo-bump-exercise.md#ov-through-adopt-edit-retry-and-submit---passing)):
  - **`go.toolchain_min` written as go.mod's `go` directive gives it,** `1.26.8` rather than `1.26` (finding 2, and the tbls run's 1). Done 2026-09-29 ([note](activity/2026-09-29-go-minimum-as-go-mod-writes-it.md)). It was a matter of fidelity, not the defect this item once called it: the Go PortGroup compares only the series, since MacPorts ships the newest patch release of each series, so `1.26` gated tbls exactly as `1.26.8` does, and comparing by series is right. What was a defect was the `toolchain` line read as a requirement, though Go documents it as a suggestion and the PortGroup builds with `GOTOOLCHAIN=local`: it could gate a port on systems where it builds;
  - `submit --passing --yes` refused, rather than `--yes` silently ignored (finding 7). Done 2026-09-29, as D11 decided ([note](activity/2026-09-29-passing-takes-no-yes.md)).
- **What the gh, usql, hk, and pgdog run asked for** ([review](reviews/2026-09-28-hugo-bump-exercise.md#gh-usql-hk-and-pgdog-chosen-for-untried-paths)):
  - **a dependency read as MacPorts reads it** (finding 5). Done 2026-09-29 ([note](activity/2026-09-29-read-as-base-reads-it.md)): `macports.ParseDependency` admits what Base's own two patterns admit, checked against Base's validator, and names the last field as the port. The evaluator refused mise's `port:bin/cmake:cmake`, which Base accepts: `portdepends::validate_depends_options` takes `port`, anything, then the port's name as the last field, while `eval/evaluator.go` allowed `port` only with one field after it;
  - **a license change that only moves a copyright year doesn't hold** (finding 1). Done 2026-09-29 ([note](activity/2026-09-29-read-as-base-reads-it.md)): where a license file's lines differ only in the years of copyright lines, it's said with the line, without holding; a program's source named for a license, such as usql's generated `text/license.go`, is no license file. Every changed license file held (`sourcecompare`), so usql's `LICENSE`, "2016-2025" to "2016-2026", held `bump`;
- **What a real rebase of gh asked for** ([review](reviews/2026-09-28-hugo-bump-exercise.md#submitting-hk-and-gh-with-a-real-rebase)):
  - **status credits a check of the files as they are, whichever it was** (finding 2). Status judges only the branch's latest check (`engine/status.go`), so after `restore rebase-29` put gh back at exactly check-35's files, it said "passed for older work" of check-37's rebased ones. Submit reads the newest finished check of the tree (`EvidenceFor`) and would have credited check-35, so the two disagree. Status should read what submit reads, and name the latest check only where none covers the files. Done 2026-09-29 ([note](activity/2026-09-29-checks-of-the-files-as-they-are.md));
  - rebase's next step from what covers the new files (finding 3). "Next: dockhand check, since the files it builds on have changed" is printed after every rebase (`command/verbs.go`), even where a check of the rebased files already stands, as it did for the second rebase onto the same master. Done 2026-09-29 (the same note): it names the check and says what status would;
  - the plan saying GitHub's results aren't reused (finding 1), with item 6's environment identity above. Done 2026-09-29 (the same note), as the check runs rather than in the plan, which doesn't ask providers what they are: an environment whose provider can't say builds every target, and says so where another of the check's environments could reuse.


## Decisions for the person

- **D6. How long build history is kept** (the SQL review's finding 8). Runs, executions, results, plans, revisions, and each build's recorded inputs are never removed. Inputs grow fastest, at about 3.6 KB a build: an estimated 700 MB at 200,000 builds. The review suggests the archives' cutoff: keep what a result still usable for reuse, or an open branch, refers to. A merged branch's own record stays either way, for status. The person would peel it back by weight (2026-09-29): some light records kept for 30 days, others gone once their branch merges. Reuse reads a merged branch's results for later checks, so what goes at merge is settled with item 6's reuse by content.

### Decided

- **D12. How far the comparison reads a build file** (2026-09-30, the flatbuffers run's finding 2). Holds are scoped to the build systems a port uses, which MacPorts says, and a build file whose change is only the version it names doesn't hold, as batch 9 made them. Past that, any other change to a build file holds, as flatbuffers' did for source lists and tests: CMake's text isn't read for what packaging would need.
- **D11. No batch `submit --passing`** (2026-09-29). `--passing` asks about each passing branch, as the design means it to (principle 7), and refuses `--yes`, which it had ignored, naming `submit --branch <name> --yes`, which submits one branch without asking ([note](activity/2026-09-29-passing-takes-no-yes.md)).
- **D9. Holds on the dependencies dockhand writes** (2026-09-29). `update` writes `go.vendors` and `cargo.crates` itself, and a check builds with only what the port declares, so what changes in them doesn't hold:
  - Go: no dependency holds at all. The `go.toolchain_min` check stays, and holds as D4 has it;
  - a new Rust `-sys` crate is listed for the person's attention, without holding. It can link a library MacPorts provides where one is installed, and a bundled copy where it isn't, which a clean check can't tell apart, so the port may want to declare the library;
  - the lines that don't hold are a count in the update's own output, such as "go.mod: 2 added, 4 moved", and nothing in the pull request.
- **D7. `update --plan` on a branch dockhand doesn't track** (2026-09-29): it plans on master unless what's checked out here changes the port, in commits since master or uncommitted edits, and refuses only then, naming `adopt` and `--new --plan`. Master's own uncommitted edits to the port are treated the same way, and `update` without `--plan` acts as it does on master.
- **D8. The GitHub CLI for marking ready** (2026-09-29). When an organization refuses dockhand's app, `submit --ready` marks the pull request ready with `gh pr ready` where `gh` is installed and signed in, and says it did; otherwise it says how to finish, as now. The organization's owners can approve dockhand's app instead, which the guide explains how to ask for.
- **D10. A failure's likely cause** (2026-09-29). Beside MacPorts' own `Error:` lines, a failure's summary gives the first compiler error in the port's log, in the format clang documents, marked as read from the log. Classifiers for other causes can follow, best effort, in a package of their own.
- **D2. Modelled contexts default to the Xcode profile** (2026-09-29), as MacPorts' builders are set up. A context that states its tools is modelled with them, as before. This replaces the tools profile the oracle settled on 2026-09-26.
- **D3. Tahoe's Xcode** (2026-09-27), settled by Xcode images following MacPorts' buildbots, below. Tahoe's image has 26.6, its buildbot's, and a `--rebuild` keeps to that rather than choosing Xcode 27.
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
- **Running what a port installs** (the certigo run's finding 1). Each executable a port installs would be run with `--version` in the guest after install, with advice, never a failure, where the output lacks the port's version. certigo 1.18.1 printed `(devel)`, since its version moved to Go's build info, which a tarball build lacks. Every check passed, and only the binary showed it.
  - Running built programs unattended has side effects to design for first: some start servers or wait on input, so each needs a timeout and no input, and a port with many executables would flood the result. The executables would come from MacPorts' own `port contents`.
  - Not before dockhand's core capabilities are solid, as the person decided on 2026-09-28.
- **Expiring credentials.** This adds access and refresh tokens with their expirations, and refresh across processes, before any login flow that needs them.
- **Evidence across repository registrations.** One database serves several checkouts. Once reuse keys evidence by content (item 6), what's left is whether identical inputs from another checkout are trusted.

## Maintenance

- **MacPorts compatibility.** Evaluator tests pass on Base 2.12.6, and CI runs them against the package of whichever release MacPorts currently points its users at. Version adapters follow the [Base design](macports-base-design-prospective.md): the current release plus a pinned master preview. Keep Base and PortGroup compatibility distinct ([evidence](macports-compatibility.md)).
- **Images.** Twelve images are made and checked: base and Xcode for macOS 12, 13, 14, 15, 26, and 27. Re-probe the facts table when an image is rebuilt; the 2026-09-27 probes found every earlier row unchanged. Golden Gate needs Tart 2.39.0 or newer.
- **Package boundaries.** Extend import checks when touching a boundary that matters; the command boundary test, and item 4's engine boundary test, are the models. Don't split packages by size alone. CI runs `make deadcode`, which fails on a function nothing reaches, tests included. Keep test-only exports documented as such. After a milestone, a sweep runs the tool without `-test`: what it lists, only tests reach, and each is test support, documented as such, or dead. The tool can't see an unused exported method on a type that reaches an interface or reflection, so a sweep checks those by references too.
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

**The [hugo exercise](reviews/2026-09-28-hugo-bump-exercise.md)** of 2026-09-28 was checked against the code at `fdf8147f` ([note](activity/2026-09-28-hugo-exercise-fixes.md)).
- **Its eleven findings hold,** each as the review describes it. Finding 11 is narrower: `status` shows CI once something has read it from GitHub, and plain `status` never does.
- **Taken,** each in its own commit: 1 for a `+dirty` build, and 2 to 9 and 11. The other session had already taken 10, ticking enhancement for an update dockhand made.
- **Its improvements** are smaller items, with the duckdb exercise's.

**The chezmoi run** in the same review, `dockhand bump` with `b8915f15`, was checked against the code at `4bb16db3`.
- **Finding 1 holds, and is older than the comparison's rewrite.** `sourcecompare` leaves go.mod's indirect requirements out, as `archive`'s comparison did before it, so a module that becomes direct reads as added. That false hold is fixed ([note](activity/2026-09-28-go-module-promotion.md)). Its broader claim, that no go.mod change can need a Portfile edit, went too far then: a module new to the build can need a library from MacPorts, as a cgo one can. D9 has since decided no Go dependency holds, since a cgo module links its library or fails, which a check catches.
- **Finding 2** is noise rather than a defect: the comparison reads every manifest it knows, and nothing tells it which the port builds with. A smaller item, as is finding 5's legend.
- **Finding 3** is a decision for the person, D7: the refusal is deliberate.
- **Findings 4 and 6** need nothing: 4 is by design, and 6 was an older build's, as the review now says.

**Re-submitting sshuttle** in the same review, after its move to Python 3.14, was checked against the code at `259ee3ba`.
- **All three findings hold.** A saved tidy plan is JSON (`TidyPlan.Save`), as it has been since it was added, while the guide calls it `plan.toml`. Applying one prints subjects and the saved notes, never a message's body. The note on a subject's source says "your commit" of any commit. A re-submit merges only the Type(s) and Tested on down (`mergeBody`), so the Description written from the commit's body at opening stays as it was. Each is a smaller item.

**The certigo run** in the same review, with `d7682668`, was checked against the code at `fc1e21a9`.
- **Findings 2 to 7 hold,** each as described; each is a smaller item. Finding 1 is a new check to design rather than a defect, and is under Later: running built programs has side effects to design for first, and the core comes first.
- **Confirmed fixed:** the saved plan as TOML with plain-text messages, `tidy --apply` showing the body it applies, and the Description from the commit's body on the first submit.

**The beekeeper-studio run** in the same review, with `de693086`, was checked against the code at `60995fad`.
- **Findings 2 and 4 hold** as described. The guest's summary of a failure is MacPorts' last three `Error:` lines (`why` in the Tart guest's script). Tested on lists every planned environment, filling in an empty observation where nothing ran, and submit's plan says "passed on" of all of them.
- **Finding 3 holds, with a cause the review didn't see:** MacPorts itself defaults `known_fail` to yes where `platforms` exclude the host (`port1.0/portutil.tcl`), and `ineligible` reports it as though the port declared it. It joins build eligibility's move to `macports`, in item 6.
- **Finding 1 is narrower than stated.** The top-level `package.json` is read, its dependencies and devDependencies. What isn't read is a manifest below the top level, a workspace's included, `.nvmrc`, or a lockfile. It joins the chezmoi run's item on which manifests the port builds with.
- **Confirmed fixed:** tidy keeping the update's subject over a person's edit, the certigo run's finding 2.

**The later runs** in the same review were checked against the code at `8c66f470`: cleaning up duckdb-cxx14, ov, git, and the cleanup after beekeeper-studio and ov, each with `11fb35f9`, and yq, with `24aa38fa`.
- **Hold as described:** yq's 1 and 2, git's 2, 3, and 6, ov's 1 (which yq's 4 repeats) and 6, and duckdb-cxx14's 1. yq's 1 is worse than it says: a check on GitHub alone, which fetched the man page from GitHub, passed, so the update would have been submitted without the line.
- **Hold, narrower or wider:**
  - git's 1: computed checksum names work, as yq's did. What's refused is a checksum declared in a variant, which can't be located in the Portfile, and the reason is lost on the way.
  - git's 4 is wider: any plan made on master says it kept the branch.
  - git's 5 is narrower: tidy names a bump made by hand, but read git-devel's `github.setup` line as git's version.
  - ov's 2: the raise compares by series, as the Go PortGroup does, which is right.
  - ov's 3 is narrower: the JSON says the tests were none, and the pull request leaves out its tests item.
  - ov's 4: check-23 was the first check on macOS 15 at that master, so the premise, another release's index, doesn't carry over.
  - ov's 7 is wider: `--yes` is silently ignored with `--passing`.
- **Deliberate:** ov's 5, the prefix `--passing` kept since its lines have no heading (4315d6bd); ov's 7, a look at each branch (principle 7), which D11 keeps; duckdb-cxx14's 2, the Git branch clean keeps for an archived branch's unmerged work.
- **Don't hold:** yq's 3 and git's 7, a failed or stopped check exiting 0. A failed check under `update --submit` exits 2, as a test pins, and one stopped by another's `--replace` exits 130; the 0 was the session's: zsh's `time` before a pipeline collapses `$pipestatus` to one element, and the session withdrew both.
- **Fixed:** the cleanup's 1 and 2 (`d1527ecd`, `8c66f470`).

**The tbls and flyctl run** in the same review, `update --outdated` with `d9065492`, was checked against the code at `ac15806c`.
- **Finding 1 holds as a fact, not as a defect.** `GoRequirement` gave go.mod's series, so `go 1.26.8` became `1.26`; but the Go PortGroup compares only the series, so the gate was the same (corrected 2026-09-29, when this roadmap had first called it a defect). It's fixed with the ov run's finding 2.
- **Finding 2 doesn't hold:** above the summary, each branch has a line of its own naming it, "✓ tbls-y0bv: 1.95.0 → 1.96.1, one commit, check-31 queued", as a test pins; the summary counts them.
- **Confirmed working:** D9's count, "go.mod: 14 moved", and a queued check replaced saying "Canceled check-30 before it started."

**The gh, usql, hk, and pgdog run** in the same review, with `c42d0587`, was checked against the code at `d3107f88`.
- **Findings 1, 2, 4, and 5 hold.** Finding 1 is wider: a file one level down whose name begins "license" is read as a license, Go source included. Finding 2's cause is the two sides' contexts: the current archives are this Mac's plan, the new ones every context's. Finding 5 is dockhand's, not the Portfile's alone: Base's own pattern admits `port:` with any middle field.
- **Finding 3 holds, and the refusal is deliberate:** a `cargo.crates_github` declaration that differs from what the generator gives the old version is a maintained override. Carrying its label over is coverage, not a defect.
- **Finding 6** sizes item 7's first bullet.

**Submitting hk and gh, with a real rebase,** in the same review, with `c42d0587`, was checked against the code at `f2f0c75d`.
- **All three findings hold.** GitHub's results are never reused because no provider but Tart records an identity or inputs, which reuse requires (`engine/reuse.go`); nothing says so. Status judges the latest check only, while submit credits any finished check of the tree. Rebase's next step is one fixed line.

**The libuv, sqlit-tui, ouch, and s2n-tls run** in the same review, with `c42d0587`, was checked against the code at `b2ae8888`.
- **Findings 1 to 7 hold,** each as described. `OpenPullRequests` has one caller, submit's plan. `review` applies the commit and Portfile rules only, and Design v3 §6.11 says more: that it evaluates and lints, and that `--check` builds; it says itself that it doesn't lint or build. impact names the first three dependents in its list. `--baseline` with `--plan` is refused by cobra's group rule. `BaselineWorthy` takes failed targets only, and `baselineWords` compares outcomes, which advisory test failures leave passed. `Regroup` reads only the numbers it's given. `check --variants`, which the design promises, doesn't exist.
- **Finding 8** is s2n-tls's Portfile, not dockhand's.
- **Placed:** 1 and 6 in batch 6, 4 and 5 in batch 7, 2 and 3 in batch 11, and 7 as item 8. The run also showed batch 4's Python pin met by the branch's own update of the dependency, which shouldn't hold.

**The flatbuffers, nuspell, zola, and alertmanager run** in the same review, with `c42d0587`, was checked against the code at `4df46bb7`.
- **All four findings hold.** Dependents come from the port index at the base (`LinkedPorts`), which records default variants' dependencies only. Every changed build file holds (`sourcecompare`), whatever builds the port. A tracked branch's worktree is recorded only as it's adopted, so one added later isn't found, and `adopt` offers only `git switch`. Rebase replays commits as they are.
- **Placed:** 2 as batch 9, taken before item 6, since it's now the most common false stop; 3 and 4 in batch 10; 1 in batch 11. How far a build file is read is D12.
- **Not dockhand's:** zola 0.23.6's aws-lc-sys failure.

**Adding py-flatbuffers to #35044** in the same review, with `c42d0587`, was checked against the code at `8c8863a0`.
- **All three findings hold.** An unscoped checksum refresh let only the selected port's checksums change (`fidelity.Checksums`), so a family's shared declaration was refused; fixed the same day. The 404's words are the download's, which doesn't know a PyPI release's files. `ApplyTidy` writes every group as a new commit, with the committer's time now.
- **Placed:** 1 with batch 5, done; 2 in batch 8; 3 in batch 10.

**create, for txt** in the same review, `create https://github.com/ErikHellman/txt --new` at `a91728ee`, was checked against the code at `a8d92e2b`.
- **All six findings hold,** each as described. Finding 1 is wider: `archives.CheckPolicy` refuses every port with vendored crates or Go modules, so `checksums` can't refresh any of them, while `update` strips and restores the blocks around the same step.
- **Placed:** all six as batch 16, taken after item 6 for finding 1.

**create, for txt, again, and dua-cli** in the same review, with `a2de6992`, was checked against the code at `96f151be`.
- **Five of the six earlier `create` findings are confirmed fixed;** the sixth, the category, is this run's finding 1. dua-cli 2.45.1 went from update to submit without a stop (#35060).
- **Findings 1, 2, 3, and 6 hold,** each as described: `refuseExisting` advises `update` for any existing port; `captureMode` asks for any branch checked out elsewhere with edits, commits or none; tidy's plan with a hand-edit note isn't unambiguous; the summary loops over every regenerated block, zero or not, and the upstream count names nothing.
- **Finding 4 holds in part.** The description should say what the port is; but no Type ticked is right for a new port, which MacPorts' template says its automation detects as a submission.
- **Placed:** 1, 2, 3, 4, and 6 as batch 18, taken next; 5 with item 7's `create` work.

**s2n-tls, and `check --variants`,** in the same review, with `f7e0e3bb`, was checked against the code at `f9d61a82`.
- **Findings 1, 2, and 3 hold, and are fixed at once** ([note](activity/2026-09-30-targets-built-from-source.md)):
  - the guest installed each target without `-s`, so MacPorts took a published archive for any target of a released version, revision, and variants, `--also` dependents most of all, and a check passed what it never built;
  - it kept each target's work, which the port's next variant build then refused;
  - a target whose tests passed didn't say so.

  `VerifierProtocol` 2 ends the standing and reuse of every result the old guest recorded.
- **Finding 4 narrows the txt run's finding 2,** batch 18's second item: the question comes only with `check --branch` from another checkout, which the item already says.
- **The fix was confirmed live** by check-54: all three of s2n-tls's builds cleaned, configured, built, and staged, with no archive fetched. Its three notes on status after the protocol change:
  - the reason said the image was made again, when dockhand's building changed: a provider now says what changed where it can (`buildenv.IdentityExplainer`), as Tart says a new guest protocol, and status and submit use its words;
  - the CHECKS column said "passed for this commit" while the attention list asked for another check: it says "passed, but needs another check" now. Both fixed the same day ([note](activity/2026-09-30-targets-built-from-source.md));
  - declined, as the run offered it only as an option: keeping results whose logs show a build. A log's text isn't a documented MacPorts interface, and checking again is the one-time cost of a change that has already happened.

**The roadmap's smaller items were re-batched on 2026-09-29,** after batches 1 to 3: by area and in order, rather than by the run that found each, which the done items keep.

**The cleanup** in the same review, `dockhand clean` at `0ffed143`, was checked against `42bcaf3a`.
- **Findings 1 and 2 hold, and are fixed** ([note](activity/2026-09-28-status-after-clean.md)): status took clean's removal of a merged branch for a lost one, and couldn't find a merged record by name.
- **Finding 3 holds:** branches from before v3 go unreported. With the review's sorting of them, it's a smaller item.

**The sshuttle run** in the hugo exercise's review, with `0ffed143`, was checked against the code at `2aa25a54`.
- **Findings 1 to 5 and 8 hold.** Finding 1 is wider than PyPI: the current version's archives are found by a reading of `master_sites` that refuses every MacPorts mirror group, while the new version's come from MacPorts' own fetch URLs. It holds about a third of the tree, so it's first among the run's items. Finding 2 is GitHub's GraphQL mutation for marking ready, refused for dockhand's OAuth app by the macports organization, and passed on without a way forward.
- **Finding 6 is wording:** serve's capacity counts checks, and a Tart check builds two releases at once, as designed.
- **Finding 7 is half right:** clean removes a check's fork branch once the branch is merged, but it stays on the fork while the branch is open.
- **Finding 9** is the `logs --port` item, already a smaller item.
- **Finding 1's fix was confirmed live** at `8a68d406`, against a scratch copy of the database. PyPI ports are compared: py-pipdeptree 4.2.5's Rust rewrite held on its new Cargo dependencies and meson.build, and sqlit-tui 1.6.4 showed a moved pin. That run found two more items, the pin that should have held and a repeated notice, now among the run's items.

**The [SQL review](reviews/2026-09-28-sql-review.md)** of 2026-09-28 read `02a4d318`. Its fixes landed as `b140a961` to `42bcaf3a` ([note](activity/2026-09-28-sql-review-fixes.md)), and were checked here against the code.
- **Done:** findings 1 to 7, 9, and 10.
  - History's questions are read through indexes (schema 23), which tests hold the queries to.
  - Archives are pruned in one statement.
  - A result's references are checked rather than read, and reuse's candidates come with their builds.
  - Commits reach the disk (`fullfsync`, as the review recommended), and the planner keeps its statistics.
  - An execution's uniqueness includes its developer tools (schema 24).
- **Left, as the review allows:** caching plans inside the store, which finding 5 takes only with a measured need.
- **Finding 8 is a decision for the person,** D6: how long build history is kept.

**The [private-helper review](reviews/2026-09-28-private-helper-ownership.md)** of 2026-09-28, by Codex, read `7be0dc2d` and was checked again at `dd21ac87` ([note](activity/2026-09-28-private-helper-review-reconciled.md)).
- **All ten findings hold.** All seven of its probes fail.
- **Worse than it ranks:** finding 3, which weakens the guardrail unattended submission relies on.
- **Taken:**
  - before item 6 goes on: 3, 4, 5, 7, 9, 10, and the table;
  - inside item 6: 1, 2, and 6, where the code they move is already moving;
  - with the Tart smaller item it revalidates: 8.
- **Kept as it says:** `newport.licenses` stays until a second consumer, and the plist writers stay separate.

**Its [follow-up](reviews/2026-09-28-private-helper-follow-up.md)** of 2026-09-28, also by Codex, read `259ee3ba` and was checked at `11fb35f9`, where nothing it cites had changed ([note](activity/2026-09-29-private-helper-follow-up-reconciled.md)).
- **Its table holds:** five findings addressed, and four open where the roadmap schedules them, each still in the code where it says.
- **Its remaining gap holds.** Both of its cases compare as nothing when the gap is in the old version, and hold when it's in the new. Fixed 2026-09-29.
- **Its criteria for the scheduled items** join item 6's entries. What it says of archive signing and SSH readiness, their items already say.

**The [helper-ownership review](reviews/2026-09-30-helper-ownership.md)** of 2026-09-30 read `fe03fa1a`, and was checked at `85df763e`, three commits later ([note](activity/2026-09-30-helper-ownership-review.md)).
- **All five findings hold,** and all seven probes still fail. Finding 4 is worse than it says: item 8's planning now calls `PortInfo.Variants`, which it found had no production caller. The duplicated baseline predicate, the prepared-port readers, and the English messages that deduplicate the comparison's findings are as it says.
- **Found beside it:** the table's category row is a live bug, not only an ownership gap: `create --category _resources` would write the port there.
- **Placed:** findings 1 to 4 and the table's first three rows as batch 17, taken next; finding 5, the update assessment in `macports`, and finding 1's shared manifest reader with the table's build-system row, as the two boundaries taken before batch 13, which adds manifests. The reader takes `dependency.GoBinary`, which is what a project builds, not a dependency.
- **Kept as it says:** planning, `binaryarchive`, and `preparation`'s embedding stay; the evaluation report and eager engine assembly stay declined.

**Earlier reviews** were triaged in the previous roadmap, which records what each contributed and what was declined ([v2/roadmap.md](v2/roadmap.md#review-triage-and-validation)).
