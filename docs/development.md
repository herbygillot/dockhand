# Development and implementation notes

This document collects build, testing, and implementation details previously kept in the introductory README.

## Build and test

Run `make` (or `make build`) to build `./dockhand`. Use `make test`, `make test-race`, `make vet`, `make fmt-check` (which names any Go file outside `vendor` that `gofmt` would change; CI runs it), and `make deadcode` (which names any function nothing reaches, tests included; CI runs it too) for checks, and `make clean` to remove the binary. Override the output with `make BINARY=/path/to/dockhand` or the Go executable with `make GO=/path/to/go`.

`dockhand --version`, and the line under the logo in the main help, report the version Go stamped from the nearest `vX.Y.Z` Git tag: the tag itself on a clean tagged checkout, `+dirty` with uncommitted edits, and a pseudo-version naming the commit past a tag. A build without Git data, such as one from a release tarball, has nothing to stamp and would say `devel`; name the version there with `make VERSION=0.9.0`, or directly with `go build -ldflags "-X github.com/herbygillot/dockhand/internal/buildinfo.Version=v0.9.0" ./cmd/dockhand`, which is what a Portfile's `-ldflags` would pass. The leading `v` is added when missing. A stamped tag always wins over that variable, so a checkout build cannot be mislabeled by a stale one. The version is what a commit's `Generated-By` trailer names, so a build that will make commits for a real pull request comes from a pushed commit, in a clean worktree of it: an untracked file alone makes the build `+dirty`, and a commit later rebased away names a build nobody can find. Tidy warns when its commits would name a `+dirty` build, and submit's preview flags a commit that does, and one naming a build whose commit GitHub's `herbygillot/dockhand` doesn't have, which pushing the commit there settles. Tests cover workflow recovery, SQLite transactions, separate driver processes, repository isolation, CLI configuration, and Tcl syntax. Git is required by repository fixtures. MacPorts integration tests run when `port-tclsh` is available and otherwise skip; VM providers, credentials, and network access are not required. SQLite uses the pure-Go `modernc.org/sqlite` driver.

The module requires Go 1.27.1 or newer. Dependencies are vendored: the `vendor` directory holds every module the build uses, so a build needs no module download and no network, and `go build`, `go test`, and `go vet` use it automatically. The Makefile sets `-mod=vendor`, so a missing or stale vendor directory fails loudly rather than downloading modules. After changing a dependency in `go.mod`, run `make vendor`, which tidies and refreshes the directory, and commit `go.mod`, `go.sum`, and `vendor` together; `make vendor-check` reports whether they agree without modifying anything, and is what a review or CI step should run. The `deadcode` target runs a tool by version through `go run`, which fetches the tool as a module of its own, so that target clears the vendor mode.

The syntax package has `FuzzParse` and `FuzzSplitList` targets; their seed cases run in ordinary tests.

`make mutate` runs go-mutesting by version over `MUTATE`'s packages or files (reuse and commitrules by default): each mutant, a small change to the code, runs its package's tests through `go test -overlay`, so the checkout is never edited, and an escaped mutant is a change no test noticed. `MUTATE_RUN` narrows the tests each runs, which engine needs, since its whole suite takes minutes a mutant: `make mutate MUTATE=internal/engine/clean.go MUTATE_RUN='^Test(Clean|Legacy)'`. `MUTATE_TIMEOUT` bounds a mutant that loops. It isn't in CI. Its `report.json` is ignored.

Tests that stand in for an external tool, a fake `tart`, `git`, `gh`, or `portindex`, write it with `testsupport.WriteExecutable`, never with `os.WriteFile` and an executable mode. Tests run in parallel and start commands constantly; a child forked while the program is still open for writing keeps a copy of the descriptor until it execs, and running the program then fails with "text file busy". The helper writes while holding `syscall.ForkLock`, so no fork can start in that window. A script that only an interpreter reads, such as a Tcl file passed to `tclsh`, does not need it.

Engine's tests run in parallel (`t.Parallel`), and `make test` runs the command line's as `COMMAND_SHARDS` processes of its test binary (one for each two cores, at least one and at most six, `tools/shard-test.sh`) beside every other package, since its tests set package-level seams and `HOME`; a plain `go test ./...` runs them in one process, as before. CI's three-core runner gets one shard and its four-core one two. On an 18-core Mac, engine went from 338 s to 115 s and command from 280 s to 61 s.

New tests follow two rules, so the suite stays parallel. A new engine test runs beside any other, and calls `t.Parallel`: it calls no `t.Setenv` or `t.Chdir` and writes no package-level variable, and what differs per test goes in through the engine it builds. A new command-line test adds no package-level seam of its own, such as another `testForge` or `testHTTPS`; it uses the ones there. Tests share their stand-ins rather than writing their own: GitHub is `forgetest.GitHub`, one stateful fake whose fork is a local bare repository, which tests set up and read rather than script call by call; fixture repositories are made with `testsupport.Git`, which keeps the person's git configuration, hooks, and signing out; and asking a URL over HTTPS is `testsupport.HTTPSAnswers`. Nothing generates mocks.

## Real VM acceptance test

The opt-in acceptance test builds two ports of a real ports tree in a real clone of dockhand's Tahoe image, `dockhand-base-tahoe`, in dockhand's Tart home: `tree`, with no dependencies, and `pv`, whose dependency the guest installs first. It needs a Mac with that image, made by `dockhand setup tart tahoe`, and a free VM slot, and it takes several minutes. It checks that both pass, that each left a log, and that the clone is deleted:

```sh
DOCKHAND_TEST_TART_LIVE=1 DOCKHAND_TEST_PORTS_TREE=~/Source/macports-ports \
DOCKHAND_TEST_MACPORTS_TCLSH=/opt/local/bin/port-tclsh \
go test -v ./internal/buildenv/tart -run '^TestLiveCheckInATahoeGuest$' -timeout 30m
```

## Scratch runs

A run of dockhand on a scratch clone, a measurement or a replay, sets three things, not one: `MACPORTS_TREE` to the clone, `DOCKHAND_DB` to a database of its own (a `.backup` copy for a replay, with `~/.dockhand/archives/` copied beside it), and `DOCKHAND_CONFIG` to a configuration whose `worktrees` is a directory of its own, as `tools/acceptance/quick.sh` writes one. `MACPORTS_TREE` picks the tree, not where its branches' worktrees go: the worktree root stays `~/Source/macports-branches` whichever clone runs, so a scratch run that sets the tree alone puts its branches beside yours. A development build migrates a `.backup` copy that `DOCKHAND_DB` names without asking, though a release last opened it, and counts only the processes whose heartbeat moves in the copy; the default database asks on a terminal and is refused without one.

## Profiling

`DOCKHAND_CPUPROFILE=<file>` writes the process's CPU profile, as Go's `runtime/pprof` makes one, to the file, finished when the command ends, an interrupted `serve` included; `go tool pprof -top <file>` reads it. An idle serve's profile on dhtest's database is how the roadmap's "An idle serve costs little" finds where its time goes.

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
MACPORTS_TREE=~/Source/macports-ports DOCKHAND_TEST_MACPORTS_TCLSH=/opt/local/bin/port-tclsh go test ./tools/survey -run TestTheNoticesOverATree -v -timeout 2h
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

Help and completion open no database. [`architecture.md`](architecture.md) maps the packages, and [the roadmap](roadmap.md) says what is being built.
