# Development and implementation notes

This document collects build, testing, and implementation details previously kept in the introductory README.

## How work gets in

The kind of work decides its way in (the person's "Go for it", 2026-11-04 12:31Z). Every piece of work is an [issue](https://github.com/herbygillot/dockhand/issues), labelled by its kind and given a release's milestone once it's placed:

- **A bug** (`bug`) goes straight to implementation, is fixed on main with a test, and rides the next candidate.
- **An improvement** (`improvement`), a wording or a small change in behavior, lands when convenient, with no design.
- **A feature** (`feature`) is designed by architecture, its design in [designs](designs/), then placed in a named release: its issue gets the release's milestone, a few lines, and a link to the design. A release's features are fixed when it opens, and its candidate is cut once they land; a feature placed later goes to the next release, unless the person names this one.
- **A core change** (`core`), rearchitecting such as a package split, a new layer, or a protocol or schema change, goes in a release with no large feature, accepted as everything passing as before.
- **Cleanup** (`cleanup`), a rule moved to its owner, a duplicate removed, a file split, is taken in the change that next works in its area; a change in an area reads its [cleanup issues](https://github.com/herbygillot/dockhand/issues?q=is%3Aissue+is%3Aopen+label%3Acleanup) first.

A milestone's description says what its release is for in a sentence or a short paragraph that a reader with no context can follow: the release's theme, its three largest items, or both, and never a bare list of issue numbers (the person's word, 2026-11-11). An issue that waits on another says "after #N". An issue's body stays short, its detail in the design file; issues are public, so nothing private goes in one. An open bug or improvement doesn't hold a candidate: at the cut, its milestone moves to the next release. Known harm does, wherever it was found and whether or not it's a regression: loss of the person's work, publication they didn't intend, an effect on a remote that isn't theirs, or unsafe execution, a review's finding counting as acceptance's does (the person's "I agree on #2, the harm rules", 2026-11-10 23:02Z). Harm, or a regression since the last release, makes another candidate; an ordinary bug rides the next release. A commit that fixes an issue closes it with `Fixes #N`. A commit for an item under an outside read, Codex's or architecture's, says `Refs #N`, and the commit that lands its last accepted fix after that read is judged says `Fixes #N` (architecture, 2026-11-12). Finished work is that commit, the closed issue, and the release's notes in [releases](releases/). What was decided, and what was declined, is in [decisions](decisions.md).

Architecture's reviews run when a release opens, each finding going to cleanup or to the next core release. Only a list that carries features gets a coalescing run; its bugs and improvements become issues as they are. Prioritization places features and opens the issues for what reaches it. Unplaced work is the open [issues with no milestone](https://github.com/herbygillot/dockhand/issues?q=is%3Aissue+is%3Aopen+no%3Amilestone); the oracle's remaining phases ([#20](https://github.com/herbygillot/dockhand/issues/20)) and the host-independence foundations ([#21](https://github.com/herbygillot/dockhand/issues/21)) run on the Mac as it allows, independent of a release's order.

After every release, before the next release's features are fixed, prioritization goes through every open issue with no milestone and brings the person a short list, each issue with a recommendation: a milestone, closed as not planned, or, for a bug, a reason to stay unplaced. The person decides; nothing is placed or closed on prioritization's word, and a decline that is a decision gets its line in [decisions](decisions.md). That round, architecture's review, and fixing the release's features happen in one sitting. A `feature` or `improvement` issue that has been through two rounds without a milestone is closed as not planned, unless the person names it. A `bug` that still reproduces is never closed by age: each round it gets a milestone or a stated reason to stay unplaced, and closing it takes the person's word. `cleanup` issues are exempt: the round closes only those whose code is gone. The first round follows v0.8.0's release (the person's ask and "Agreed", 2026-11-10 17:33Z and 17:34Z; the bug rule at 23:02Z).

This replaced docs/roadmap.md (the person's "Yes, please do so", 2026-11-10 15:53Z, on whether to keep a roadmap file at all); the roadmap as it last stood is in Git's history.

## Build and test

Run `make` (or `make build`) to build `./dockhand`. Use `make test`, `make test-race`, `make lint` (golangci-lint, whose govet runs `go vet`'s analyzers; CI runs it), `make fmt-check` (which names any Go file outside `vendor` that `gofmt` would change; CI runs it), and `make deadcode` (which names any function nothing reaches, tests included; CI runs it too) for checks, and `make clean` to remove the binary. Override the output with `make BINARY=/path/to/dockhand` or the Go executable with `make GO=/path/to/go`.

CI (`.github/workflows/ci.yml`) runs on a push to main, a `release/` branch or a `v` tag, and on a pull request, where it leaves out the corpus and Intel jobs; any other branch's push runs none, as its pull request or `make gate` tests it. It keeps one run at a time on each branch, pull request and tag: a newer push cancels the older run on the same ref, so a busy stretch on main tests its last commit, and a commit in the middle of it has no result of its own. A commit that touches only `docs/designs` or `docs/decisions.md` runs no CI, since no test reads them; the docs workflow (`.github/workflows/docs.yml`) runs `make path-check` on Linux for any change to the docs, the tools or the Makefile.

`dockhand --version`, and the line under the logo in the main help, report the version Go stamped from the nearest `vX.Y.Z` Git tag: the tag itself on a clean tagged checkout, `+dirty` with uncommitted edits, and a pseudo-version naming the commit past a tag. A build without Git data, such as one from a release tarball, has nothing to stamp and would say `devel`; name the version there with `make VERSION=0.9.0`, or directly with `go build -ldflags "-X github.com/herbygillot/dockhand/internal/buildinfo.Version=v0.9.0" ./cmd/dockhand`, which is what a Portfile's `-ldflags` would pass. The leading `v` is added when missing. A stamped tag always wins over that variable, so a checkout build cannot be mislabeled by a stale one. The version is what a commit's `Generated-By` trailer names, so a build that will make commits for a real pull request comes from a pushed commit, in a clean worktree of it: an untracked file alone makes the build `+dirty`, and a commit later rebased away names a build nobody can find. Tidy warns when its commits would name a `+dirty` build, and submit's preview flags a commit that does, and one naming a build whose commit GitHub's `herbygillot/dockhand` doesn't have, which pushing the commit there settles. Tests cover workflow recovery, SQLite transactions, separate driver processes, repository isolation, CLI configuration, and Tcl syntax. Git is required by repository fixtures. MacPorts integration tests run when `port-tclsh` is available and otherwise skip; VM providers, credentials, and network access are not required. SQLite uses the pure-Go `modernc.org/sqlite` driver.

The module requires Go 1.27.1 or newer. Dependencies are vendored: the `vendor` directory holds every module the build uses, so a build needs no module download and no network, and `go build`, `go test`, and `go vet` use it automatically. The Makefile sets `-mod=vendor`, so a missing or stale vendor directory fails loudly rather than downloading modules. After changing a dependency in `go.mod`, run `make vendor`, which tidies and refreshes the directory, and commit `go.mod`, `go.sum`, and `vendor` together; `make vendor-check` reports whether they agree without modifying anything, and is what a review or CI step should run. The `deadcode` target runs a tool by version through `go run`, which fetches the tool as a module of its own, so that target clears the vendor mode.

The syntax package has `FuzzParse` and `FuzzSplitList` targets; their seed cases run in ordinary tests.

`make mutate` runs go-mutesting by version over `MUTATE`'s packages or files (reuse and commitrules by default): each mutant, a small change to the code, runs its package's tests through `go test -overlay`, so the checkout is never edited, and an escaped mutant is a change no test noticed. `MUTATE_RUN` narrows the tests each runs, which engine needs, since its whole suite takes minutes a mutant: `make mutate MUTATE=internal/engine/clean.go MUTATE_RUN='^Test(Clean|Legacy)'`. `MUTATE_TIMEOUT` bounds a mutant that loops. It isn't in CI. Its `report.json` is ignored.

Tests that stand in for an external tool, a fake `tart`, `git`, `gh`, or `portindex`, write it with `testsupport.WriteExecutable`, never with `os.WriteFile` and an executable mode. Tests run in parallel and start commands constantly; a child forked while the program is still open for writing keeps a copy of the descriptor until it execs, and running the program then fails with "text file busy". The helper writes while holding `syscall.ForkLock`, so no fork can start in that window. A script that only an interpreter reads, such as a Tcl file passed to `tclsh`, does not need it.

Engine's tests run in parallel (`t.Parallel`), and `make test` runs the command line's as `COMMAND_SHARDS` processes of its test binary (one for each two cores, at least one and at most six, `tools/shard-test.sh`) beside every other package, since its tests set package-level seams and `HOME`; a plain `go test ./...` runs them in one process, as before. CI's three-core runner gets one shard and its four-core one two. On an 18-core Mac, engine went from 338 s to 115 s and command from 280 s to 61 s.

New tests follow two rules, so the suite stays parallel. A new engine test runs beside any other, and calls `t.Parallel`: it calls no `t.Setenv` or `t.Chdir` and writes no package-level variable, and what differs per test goes in through the engine it builds. A new command-line test adds no package-level seam of its own, such as another `testForge` or `testHTTPS`; it uses the ones there. Tests share their stand-ins rather than writing their own: GitHub is `forgetest.GitHub`, one stateful fake whose fork is a local bare repository, which tests set up and read rather than script call by call; fixture repositories are made with `testsupport.Git`, which keeps the person's git configuration, hooks, and signing out; and asking a URL over HTTPS is `testsupport.HTTPSAnswers`. Nothing generates mocks.

## The fault sweep

`internal/command/sweep` runs each workflow once clean, then once for each of its outside effects with that effect failing, recovers as a person would, and compares the world with the clean run's (docs/designs/fault-sweep.md, #129). The gate fails each effect once; `make sweep` fails each in all five ways (fail, dead, a remote reply lost, and a store transaction or ref update whose outcome is unknown, landed or not), and runs for each release.

A scenario is a function in `scenarios_test.go`: the world it starts in (`arrange`, whose command lines aren't swept), the command lines that are the workflow (`steps`), and declarations beside it rather than code:

- `recover`, where recovery isn't the steps again;
- `bestEffort`, effects by address whose failure the command may not report, each with why; one that names no effect the scenario makes fails;
- `known`, findings not yet fixed, by address and mode, each with its issue; one that stops reproducing fails, so the list only shrinks.

An effect's address is its boundary, its first call site outside the boundary's package, and which call of that site it is: `store.update@engine.(*Engine).ApplySubmit#1`. A failure prints the address, and how to list it as known. A new git subcommand in `internal/git` is classed a write or a read in `plan_test.go`, which `TestEveryGitSubcommandIsClassed` requires.

## Real VM acceptance test

The opt-in acceptance test builds two ports of a real ports tree in a real clone of dockhand's Tahoe image, `dockhand-base-tahoe`, in dockhand's Tart home: `tree`, with no dependencies, and `pv`, whose dependency the guest installs first. It needs a Mac with that image, made by `dockhand setup tart tahoe`, and a free VM slot, and it takes several minutes. It checks that both pass, that each left a log, and that the clone is deleted:

```sh
DOCKHAND_TEST_TART_LIVE=1 DOCKHAND_TEST_PORTS_TREE=~/Source/macports-ports \
DOCKHAND_TEST_MACPORTS_TCLSH=/opt/local/bin/port-tclsh \
go test -v ./internal/buildenv/tart -run '^TestLiveCheckInATahoeGuest$' -timeout 30m
```

## Heavy work on a shared Mac

Sessions that share one Mac take turns at its heavy work: `make gate`, the checks a commit passes, in CI's order, and `make test-corpus` each run under `tools/heavy`, a lock on `~/.dockhand-heavy.lock` (`DOCKHAND_HEAVY_LOCK` names another), and so does any other long run, a Tart check or a whole-tree survey: `tools/heavy <command>`. A run that finds the lock held says by whom and waits; a command run under it that runs `tools/heavy` again doesn't wait on itself. Each session also keeps its own `DOCKHAND_DB` copy, its archives cloned beside it, on a ports clone and worktrees of its own, as scratch runs below say: two databases on one checkout number their checkpoints apart and move each other's branches, so turns are about cores and memory, never one database or one checkout.

## Scratch runs

A run of dockhand on a scratch clone, a measurement or a replay, sets three things, not one: `MACPORTS_TREE` to the clone, `DOCKHAND_DB` to a database of its own (a `dockhand backup` copy for a replay, with `~/.dockhand/archives/` copied beside it), and `DOCKHAND_CONFIG` to a configuration whose `worktrees` is a directory of its own, as `tools/acceptance/quick.sh` writes one. `MACPORTS_TREE` picks the tree, not where its branches' worktrees go: the worktree root stays `~/Source/macports-branches` whichever clone runs, so a scratch run that sets the tree alone puts its branches beside yours. A development build migrates a backup copy that `DOCKHAND_DB` names without asking, though a release last opened it, and counts only the processes whose heartbeat moves in the copy; the default database asks on a terminal and is refused without one.

## Profiling

`DOCKHAND_CPUPROFILE=<file>` writes the process's CPU profile, as Go's `runtime/pprof` makes one, to the file, finished when the command ends, an interrupted `serve` included; `go tool pprof -top <file>` reads it. An idle serve's profile on dhtest's database is how "An idle serve costs little" ([#18](https://github.com/herbygillot/dockhand/issues/18)) finds where its time goes.

`DOCKHAND_HEAPPROFILE=<file>` writes the in-use heap, after a collection, as `runtime/pprof` makes one, when the command ends, and in `serve` also each time its queue empties after work, the day's look and its checks ended; `go tool pprof -sample_index=inuse_space -top <file>` reads it. Measured on the main Mac (2026-11-02, a scratch copy of the database and caches, `outdated --mine` over 1,116 ports): 458 MB at most resident, 209 MB peak footprint, and 37 MB of live heap at its end, 30 MB of it GitHub's answers the conditional cache held in memory beside its directory; the cache now keeps them on disk alone where it has a directory, and serve gives the rest back to macOS when its queue empties (`debug.FreeOSMemory`). The next rc's serve row samples an hour after its day's work, tens of MB the bar, with the profile kept beside the row.

## Other opt-in tests

Tests that need MacPorts run only against the installation `DOCKHAND_TEST_MACPORTS_TCLSH` names, taking `portindex` from beside it, and skip without it: a `PATH` that happens to reach another Base would test that one silently. They run on a Mac, and on Linux with MacPorts Base built there, where the evaluator models a Mac:

```sh
DOCKHAND_TEST_MACPORTS_TCLSH=/opt/local/bin/port-tclsh make test
```

CI runs them too, against the release MacPorts currently points its users at, as `port selfupdate` reads it, so a new release meets this code the day it's out. A new family, which the evaluator refuses until dockhand has been checked against it, stops CI until then. CI installs the release's package for the runner's macOS, checked against the release's own checksums.

The rest reach something outside the checkout and run only when named:

| Variable | Test | What it reaches |
| --- | --- | --- |
| `DOCKHAND_TEST_MACPORTS_TCLSH` | every test that evaluates Portfiles or builds an index | the MacPorts installation whose `port-tclsh` it names |
| `DOCKHAND_TEST_BASE_ADAPTER=preview` | the same | admits a development build of Base, such as master, which the evaluator otherwise refuses; the parent-side host-access regressions skip under it |
| `DOCKHAND_TEST_TCLSH` | `tcl/rpc` | a `tclsh` other than the one on `PATH`, such as a Tcl 9 `port-tclsh` |
| `DOCKHAND_TEST_PORTS_TREE` | `macports/eval` `TestPortsThatAskBaseForCompilersReadTheTable` | a macports-ports checkout, read only |
| `DOCKHAND_GUARD_TREE` | `macports/eval` `session_guard_test.go` | a ports tree whose Portfiles the session guard samples |
| `DOCKHAND_TEST_DEPENDENCY_HELPERS=1` | `macports/depblock` `live_test.go` | the installed `go2port`/`cargo2port` and an upstream Go archive |
| `DOCKHAND_TEST_GITHUB_PR`, `DOCKHAND_TEST_GITHUB_TOKEN` | `forge/github` `inspect_live_test.go` | one pull request, `owner/repo#number`, read with that token |
| `DOCKHAND_TEST_BOOTSTRAP_VM` | `tart/provision` `TestLiveAgentRegistration` | a running disposable VM you own, whose agent it registers |
| `DOCKHAND_TEST_TART_LIVE` | `buildenv/tart` `TestLiveCheckInATahoeGuest` | the acceptance test above, with `DOCKHAND_TEST_PORTS_TREE` |
| `DOCKHAND_TEST_TART_IMAGE` | `tart/host` `TestLiveTartContracts`, `tart/guestssh` `TestLiveChannel` | Tart's listing, stop, and delete behavior, and the guest channel, on a clone of the named raw-disk image |
| `DOCKHAND_TEST_TART_ASIF_SOURCE` | `tart/host` `TestLiveTartListsWhileAnASIFVMRuns` | an ASIF image such as Golden Gate's, cloned and run briefly while Tart lists its VMs; it needs Tart 2.39.0 or newer, and skips on an older one |

## The notices' measure over a tree

`tools/survey`'s `TestTheNoticesOverATree` runs the notices that hold nothing over a whole ports tree, each Portfile read as a new port's, every line one its change adds, the most each could say, and prints every match for a person to read. No snapshot is kept; a phrase or rule that reads wrong is narrowed in its code.

```sh
DOCKHAND_MEASURE_TREE=~/Source/macports-ports DOCKHAND_TEST_MACPORTS_TCLSH=/opt/local/bin/port-tclsh go test ./tools/survey -run TestTheNoticesOverATree -v -timeout 2h
```

`DOCKHAND_MEASURE_RESTATED` sets how many Portfiles `restated` evaluates (200 unless given, 0 for all) and `DOCKHAND_MEASURE_COMMITS` how many commits `ticket-trailer` reads (20,000 unless given).

| Notice | Matches | Wrong | Narrowed by the measure |
| --- | --- | --- | --- |
| A maintainer's instruction in the comments (I7) | 49 of 20,231 Portfiles | 1, cpmtools' "do not update the version number" | nothing |
| `restated` | 19 lines, of 600 Portfiles evaluated | none | nothing |
| `path-style` | 1,689, pkgconfig's 1,462 first | none | only a port the tree writes path-style, in the form it writes most (libffi's 136 and libheif's 19 had read wrong) |
| `ticket-trailer` | 10 of the last 20,000 commits | none | a trailer-shaped line in a list or without its colon counts as one, and a ticket a trailer cites is skipped |
| `license-spdx` | 29 of 20,233 Portfiles | none | WTFPL left out, which the tree writes as often as WTFPL-2 (7 to 8) |
| `noarch-platforms` | 58 | none seen | only platforms that are darwin, written or by default: go's `darwin >= …` is the maintainer's bound |
| `commented-code` | 1,085 lines | about 1 in 50, as an English sentence that starts with a Portfile word | a word that is English too (version, copy, file, …) needs Tcl after it and no word such as "with" or "the" next; a build file's name, as meson.build, isn't an option |
| `unreferenced-file` | 381 files | none seen: they're patches and select files earlier versions and subports used | a file is named by a word that builds it from variables, by its directory under files/, or by a `${filespath}` word of variables alone |

The first four were measured on macports-ports 324108c8883 (20,231 Portfiles), the last four on 9c3d4bd8a7c (20,233). Those four are what the measure says of a whole tree; a change says one only of the lines and files it adds.

## MacPorts' distributable checker

`internal/macports/distributable/distributable_lib.tcl` is
macports-infrastructure's `jobs/distributable_lib.tcl`, verbatim, at the commit
`distributable.Pin` names; `distributable.tcl` answers its `infoForPort` from
the port's evaluation and the port index of the tree it judges, the
candidate's for the change and the base's for master, read from the cache or
staged from the base's as a check's plan stages one, so its verdict is the
buildbot's for that tree. Where neither index can be read, the verdict is
unknown, and nothing is said. The weekly `distributable-master.yml` job fails when master's copy
differs. To re-pin, copy master's file over the pinned one, set `Pin` to the
commit the job names, and run `go test ./internal/macports/distributable/`,
whose table shows what changed.

## Libraries we didn't take

Dependencies are few and vendored, and the build is cgo-free, so a new module has to beat what the standard library, `vendor`, or MacPorts' and Apple's own tools already do. The library survey of 2026-10-02 checked the likely candidates, and these were declined:

| Library | What dockhand uses | Why |
| --- | --- | --- |
| a Go Tcl interpreter, semver and RE2 for vercmp and livecheck | MacPorts' own `port-tclsh` | a reimplementation would drift from MacPorts, as RE2 does from Tcl's regular expressions |
| `go-git` | the `git` command | about 15 times slower on `git status` over macports-ports, and behind on partial clones and worktrees |
| `zalando/go-keyring`, `99designs/keyring` | `/usr/bin/security` | go-keyring runs the same command without a context or an output bound; 99designs needs cgo |
| `x/crypto/ssh` | Apple's `/usr/bin/ssh` | Local Network privacy can block a Go binary's dial to a guest |
| `gofri/go-github-ratelimit` | `internal/github`'s own wait | it retries any refused request, writes included; dockhand never retries a write |
| `ulikunitz/xz`, `mholt/archives` | the system's `xz`, `zstd`, and `lzip` | pure-Go xz was 2.3 times slower; `klauspost/compress/zstd` is the one to take if a missing `zstd` ever comes up |

## State and service boundaries

v3 keeps its records in SQLite behind the `internal/store` contract, with `internal/store/sqlite` as the implementation, in `~/.dockhand/dockhand.db` unless `--db` or `DOCKHAND_DB` says otherwise. The schema is `internal/store/sqlite/schema/NNN.sql`, applied in order and only forward: a change to it is a new file, never an edit to one already released. One database holds several ports checkouts, and every read and write names the one it is for. v2's `state.db` is never read.

Two rules keep the store's writes short (the SQL study of 2026-10-13). Every foreign-key child column of a table that pruning deletes from has an index: a delete checks its foreign keys there, and without one `PruneHistory` went quadratic in history. And no write transaction runs past about a second at a year's scale, since a writer waiting for the lock gives up after five (`BusyTimeout`), where a transaction may run thirty: a large delete goes in chunks, as history's pruning does. Every prune and window statement has a plan test, and a correlated subquery that scans a table fails it.

Help and completion open no database. [`architecture.md`](architecture.md) maps the packages, and the [issues](https://github.com/herbygillot/dockhand/issues) say what is being built, each release's under its milestone.

## Maintenance

- **Release candidates.** An rc is tagged when the M1 is free to start its acceptance, after a short window that gathers what's found since the last, so fixes found hours apart make one tag, not two (the person's "Do #3, #5, #2 & #1", 2026-10-31, item 2: rc3 followed rc2 by an hour, and part of rc2's stages were done again). Once main takes the next release's work, the release's later candidates are tagged from `release/v0.N`, made at the last candidate, each fix picked onto it as it lands on main, so the last candidate's acceptance still stands for what didn't change. Known harm, wherever it was found, or a regression since the last release, makes another candidate, and an ordinary bug doesn't; "How work gets in" says what harm is (proposed 2026-10-31, adopted with the work process, 2026-11-04, its harm exception 2026-11-10).
- **Acceptance rows move with the behavior they test.** A commit that changes what a person sees changes its row in the same commit, and before it lands runs those rows on the main Mac, in the heavy-run turn, with `tools/heavy tools/acceptance/quick.sh --rows "<affected>"` (the same list, item 1: rc2's quick stage lost hours to B1, B2, and A10 expecting rc1's behavior, fixed in 013d5f70). CI's acceptance self-test proves the runner and the harm sweep against a stand-in, not the rows against the build, and a lint of the rows wouldn't have caught those three. A scheduled run of the no-VM rows against main was declined on 2026-11-11 ([#14](https://github.com/herbygillot/dockhand/issues/14)), since every candidate runs the quick stage.
- **A bug where an outside effect is recorded late, or not at all, gets a fault-sweep position, not an acceptance row.** The sweep (`internal/command/sweep`, #129) fails each effect of a workflow in turn and compares the recovered world with a clean run's, so the bug's scenario covers it, and its fix takes its address off the known list. It gets an acceptance row only where it needs a real kill, real GitHub, or real Tart; the fault-kit rows that do (B11, D-I3, D-I4, D-I5, D-I7, D-N1, D-N3, D-R2, D-R4, D-T1, D-T5, F8, G10, G21, G38, S1) stay (the RC acceptance review, 2026-11-10, #145).
- **MacPorts compatibility.** Evaluator tests pass on Base 2.12.6, and CI runs them against the package of whichever release MacPorts currently points its users at, and a weekly job runs them against base's master (231f48c1). Version adapters follow the Base design prospective (2026-09-24): the current release plus a pinned master preview. Keep Base and PortGroup compatibility distinct ([evidence](macports-compatibility.md)).
- **Images.** Twelve images are made and checked: base and Xcode for macOS 12, 13, 14, 15, 26, and 27. Re-probe the facts table when an image is rebuilt; the 2026-09-27 probes found every earlier row unchanged. Golden Gate needs Tart 2.39.0 or newer.
- **Package boundaries.** Extend import checks when touching a boundary that matters; the command and engine boundary tests are the models. Don't split packages by size alone. CI runs `make deadcode`, which fails on a function nothing reaches, tests included. Keep test-only exports documented as such. After a milestone, a sweep runs the tool without `-test`: what it lists, only tests reach, and each is test support, documented as such, or dead. The tool can't see an unused exported method on a type that reaches an interface or reflection, so a sweep checks those by references too.
- **Relocations.** A batch that moves a rule to its owner lists, in its note, the call sites it switched to the owner's rule, and closes with a grep across internal/ for the old rule's shape, its predicate, sentence, or switch, deleting what it finds or saying why it stays. Each of the 5897a82 review's high findings would have been caught by that grep on the day (its section 5). A rule is moved only once every consumer carries the facts it needs and stops judging beside it: findings 3, 4, 6, and 9 of Codex's review of 8e314b96 are a consumer judging beside its owner, so a move adds an agreement test across the consumers, check, review, status, submit, ship, and serve, where they apply. The engine was 24,704 lines in 68 files at 8e314b96; it's measured again at v0.7.0's rc1, and not split for size alone.
- **Dependencies.** Minor releases go on main when they build and the gate passes, as 3706d086's did; a major version is measured first, by building main against it, and is an issue like any fix (the person's "Let's go with you recommendation.", 2026-10-24 18:31Z). A new Go library is taken where it makes sense, to gain major functionality, improve performance, or eliminate logic dockhand keeps repeating (the person's "Change the dependencies rule: I'm open to new Golang libraries and dependencies where it makes sense…", 2026-10-31 23:48Z, replacing the library survey's preference for none), weighed in the item that brings it: what it replaces, the modules it brings with it, each a go.vendors entry in dockhand's port, and its maintenance.
- **GitHub's budget.** Requests to its API are paced at 750 a minute, under its documented 900. A whole `outdated --mine` spends about 1,800 of the 5,000 an hour the person's account has, a budget shared with the GitHub CLI.
- **Test throughput.** The cost unit is the MacPorts interpreter. Keep per-test timing visible, and prefer simulated clocks for time-driven tests; v2's `workflow` tests show what wall-clock budgets do under load.
