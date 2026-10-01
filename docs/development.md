# Development and implementation notes

This document collects build, testing, and implementation details previously kept in the introductory README.

## Build and test

Run `make` (or `make build`) to build `./dockhand`. Use `make test`, `make test-race`, `make vet`, `make fmt-check` (which names any Go file outside `vendor` that `gofmt` would change; CI runs it), and `make deadcode` (which names any function nothing reaches, tests included; CI runs it too) for checks, and `make clean` to remove the binary. Override the output with `make BINARY=/path/to/dockhand` or the Go executable with `make GO=/path/to/go`.

`dockhand --version`, and the line under the logo in the main help, report the version Go stamped from the nearest `vX.Y.Z` Git tag: the tag itself on a clean tagged checkout, `+dirty` with uncommitted edits, and a pseudo-version naming the commit past a tag. A build without Git data, such as one from a release tarball, has nothing to stamp and would say `devel`; name the version there with `make VERSION=0.9.0`, or directly with `go build -ldflags "-X github.com/herbygillot/dockhand/internal/version.Version=v0.9.0" ./cmd/dockhand`, which is what a Portfile's `-ldflags` would pass. The leading `v` is added when missing. A stamped tag always wins over that variable, so a checkout build cannot be mislabeled by a stale one. The version is what a commit's `Generated-By` trailer names, so a build that will make commits for a real pull request comes from a pushed commit, in a clean worktree of it: an untracked file alone makes the build `+dirty`, and a commit later rebased away names a build nobody can find. Tidy warns when its commits would name a `+dirty` build, and submit's preview flags a commit that does, and one naming a build whose commit GitHub's `herbygillot/dockhand` doesn't have, which pushing the commit there settles. Tests cover workflow recovery, SQLite transactions, separate driver processes, repository isolation, CLI configuration, and Tcl syntax. Git is required by repository fixtures. MacPorts integration tests run when `port-tclsh` is available and otherwise skip; VM providers, credentials, and network access are not required. SQLite uses the pure-Go `modernc.org/sqlite` driver.

The module requires Go 1.27.1 or newer. Dependencies are vendored: the `vendor` directory holds every module the build uses, so a build needs no module download and no network, and `go build`, `go test`, and `go vet` use it automatically. The Makefile sets `-mod=vendor`, so a missing or stale vendor directory fails loudly rather than downloading modules. After changing a dependency in `go.mod`, run `make vendor`, which tidies and refreshes the directory, and commit `go.mod`, `go.sum`, and `vendor` together; `make vendor-check` reports whether they agree without modifying anything, and is what a review or CI step should run. The `deadcode` target runs a tool by version through `go run`, which fetches the tool as a module of its own, so that target clears the vendor mode.

The syntax package has `FuzzParse` and `FuzzSplitList` targets; their seed cases run in ordinary tests.

Tests that stand in for an external tool, a fake `tart`, `git`, `gh`, or `portindex`, write it with `testsupport.WriteExecutable`, never with `os.WriteFile` and an executable mode. Tests run in parallel and start commands constantly; a child forked while the program is still open for writing keeps a copy of the descriptor until it execs, and running the program then fails with "text file busy". The helper writes while holding `syscall.ForkLock`, so no fork can start in that window. A script that only an interpreter reads, such as a Tcl file passed to `tclsh`, does not need it.

## Real VM acceptance test

The opt-in acceptance test builds two ports of a real ports tree in a real clone of dockhand's Tahoe image, `dockhand-base-tahoe`, in dockhand's Tart home: `tree`, with no dependencies, and `pv`, whose dependency the guest installs first. It needs a Mac with that image, made by `dockhand providers setup tart tahoe`, and a free VM slot, and it takes several minutes. It checks that both pass, that each left a log, and that the clone is deleted:

```sh
DOCKHAND_TEST_TART_LIVE=1 DOCKHAND_TEST_PORTS_TREE=~/Source/macports-ports \
DOCKHAND_TEST_MACPORTS_TCLSH=/opt/local/bin/port-tclsh \
go test -v ./internal/buildenv/tart -run '^TestLiveCheckInATahoeGuest$' -timeout 30m
```

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
| `DOCKHAND_TEST_PORTS_TREE` | `macports/eval` `TestPortsThatReadTheHostThroughBaseCompilerQueries` | a macports-ports checkout, read only |
| `DOCKHAND_GUARD_TREE` | `macports/eval` `session_guard_test.go` | a ports tree whose Portfiles the session guard samples |
| `DOCKHAND_TEST_DEPENDENCY_HELPERS=1` | `macports/dependency` `live_test.go` | the installed `go2port`/`cargo2port` and an upstream Go archive |
| `DOCKHAND_TEST_PORTS_REPO` | `git` `TestCaptureRealPortsCheckout` | a real macports-ports checkout, read only |
| `DOCKHAND_TEST_GITHUB_PR`, `DOCKHAND_TEST_GITHUB_TOKEN` | `forge/github` `inspect_live_test.go` | one pull request, `owner/repo#number`, read with that token |
| `DOCKHAND_TEST_BOOTSTRAP_VM` | `tart/provision` `TestLiveAgentRegistration` | a running disposable VM you own, whose agent it registers |
| `DOCKHAND_TEST_TART_LIVE` | `buildenv/tart` `TestLiveCheckInATahoeGuest` | the acceptance test above, with `DOCKHAND_TEST_PORTS_TREE` |
| `DOCKHAND_TEST_TART_IMAGE` | `tart/host` `TestLiveTartContracts`, `tart/channel` `TestLiveChannel` | Tart's listing, stop, and delete behavior, and the guest channel, on a clone of the named raw-disk image |
| `DOCKHAND_TEST_TART_ASIF_SOURCE` | `tart/host` `TestLiveTartListsWhileAnASIFVMRuns` | an ASIF image such as Golden Gate's, cloned and run briefly while Tart lists its VMs; it needs Tart 2.39.0 or newer, and skips on an older one |

## State and service boundaries

v3 keeps its records in SQLite behind the `internal/store` contract, with `internal/store/sqlite` as the implementation, in `~/.dockhand/dockhand.db` unless `--db` or `DOCKHAND_DB` says otherwise. The schema is `internal/store/sqlite/schema/NNN.sql`, applied in order and only forward: a change to it is a new file, never an edit to one already released. One database holds several ports checkouts, and every read and write names the one it is for. v2's `state.db` is never read.

Help and completion open no database. [`architecture.md`](architecture.md) maps the packages, and [the roadmap](roadmap.md) says what is being built.
