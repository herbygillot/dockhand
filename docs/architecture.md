# Architecture

How dockhand v3 is put together, and where each part of [Design v3](design-v3.md) lives in the code. The design says what dockhand does and why; this page says where. [`usage.md`](usage.md) is the guide for people using it, and [`development.md`](development.md) covers building and testing. v2's architecture, component map, and state design are kept in [`v2/`](v2/README.md), and describe the tag `v2-final`.

## Layers

```
cmd/dockhand          main: runs the command line, exits with its code
internal/command      parses, finds the context, asks, renders text and JSON
internal/engine       decides and acts: binds a request to the checkout, Git, the store, and providers
domain packages       MacPorts, upstream releases, forges, Git, Tart, macOS
internal/store         the records, behind a contract; store/sqlite implements it
internal/model         the records' vocabulary; imports nothing from dockhand
```

`command` knows about terminals, flags, and wording. It finds the branch a request is about, from `--branch` or the worktree it runs in, and turns the engine's results into text or the `--json` envelope. It also makes the providers from the configuration (`settings.go`). `engine` writes nothing to the terminal: it takes a request, does the work, and returns what happened, and it reports progress through the store's event journal. The domain packages know nothing of branches or checks, and most of them came through the rebuild unchanged from v2.

## The records

`internal/model` defines them, and `internal/store` is the contract for keeping them (Design v3 §3):

| Record | What it is |
| --- | --- |
| `Branch` | the unit of work: a Git branch of the ports tree, its base on master, its title, its state (open, merged, closed, archived), and at most one pull request |
| `Edit` | what an authoring command did to a port, and the commit subject it leaves for `tidy` |
| `Checkpoint` | the history a `tidy` or `rebase` replaced, which `restore` puts back |
| `Revision` | an immutable candidate: a commit, or a numbered snapshot of the working files, with its tree and base |
| `Plan` | what a check of one revision covers: the changed targets, extras, and prerequisites, and each environment's own plan (`EnvironmentPlan`): its build order, dependencies, what needs Xcode, what it can't meet, and its exclusions with their reasons |
| `Run` | one accepted check request, `check-42`, of one plan on one or more environments |
| `GuestExecution` | one run's work in one environment and attempt, `tart_7y62p4sigena6xlr`, with the provider's own reference and what the guest reported of itself |
| `TargetResult` | one target's outcome and phases in one execution: passed, failed, blocked by a failed prerequisite, unmet by the environment, interrupted, or unevaluated |
| `Session`, `Lease`, `Event` | the processes sharing the database, who holds what, and the journal `watch` and `check` follow |

`internal/store/sqlite` keeps them in `~/.dockhand/dockhand.db`, in WAL mode with foreign keys on. Its schema is `schema/NNN.sql`, applied in order and only forward. Every read and write names the repository it is for, so one database serves several ports checkouts.

## A check, end to end

1. **Capture** (`engine/capture.go`). The branch's files become a revision: the working files as a numbered snapshot, the index with `--staged`, or the commit with `--head`. A snapshot is a Git tree written from the files on disk; the worktree, its index, and its refs are left as they are.
2. **Scope** (`engine/scope.go`, `engine/diff.go`). The paths the revision changes from its base decide which port directories changed, by MacPorts CI's rule. Commits never enter into it.
3. **Plan** (`engine/plan.go`, `engine/ports.go`). Each changed directory's subports are evaluated by MacPorts itself (`macports/eval`), against a port index of the revision (`macports/portindex`). Each environment evaluates them for itself, and gets its own plan: what it builds, in its own dependency order, and why not the rest. The changed ports they need in any environment come along as prerequisites. Each exclusion keeps its reason. A port that needs full Xcode, itself or through a prerequisite, is unmet in an environment without it. A result from an earlier check of the same files counts where that check planned the target in the same environment (`engine.Counts`).
4. **Environments** (`engine/provider.go`). `--on`, else `check.on`, becomes environments: a provider, a platform, and the developer tools it offers. Tart has one per release image.
5. **Run** (`engine/runner.go`). The run is queued in the store. Whoever holds its lease drives it: `check` in the foreground when no `serve` is running, else `serve`. It makes one guest execution per environment and gives each provider a `buildenv.Job`. The provider records each target's result as it finishes, and the runner judges it under the plan's test policy (`model.TestPolicy.Judge`), whichever provider built it. Trouble with the environment itself is retried, up to `model.MaxAttempts`. A verdict is never retried.
6. **Evidence** (`engine/evidence.go`, `engine/words.go`). The results across environments become what `check`, `status`, and `submit` show, and the pull request's *Tested on* (`engine/body.go`).

The providers implement `buildenv.Provider`, the contract in `internal/buildenv` for providers of build environments, and live beneath it: the `Job` a provider is given, the `Build` it records through, `ErrInfrastructure`, and the capabilities the engine looks for (`ReleaseProvider`, `Remedier`, `OwnTestsProvider`, `LeftoverProvider`). A provider imports the contract and `model`, never the engine; the command layer composes them, and a test holds that line.

- **`buildenv/tart`** clones a prepared image for each release and attempt, and deletes the clone afterwards. It stages the revision's tree and a port index into the guest (`buildenv/staging`), reaches the guest over SSH (`tart/channel`), and runs `guest.tcl` there, in MacPorts CI's order. `tart/host` controls the VMs, and `tart/provision` makes and checks the images `providers setup tart` builds. [Tart provider](tart-provider.md).
- **`buildenv/ghactions`** pushes the revision's commit to your fork and reads MacPorts' own workflow run. [github provider](github-provider.md).
- **`buildenv/script`** hands a request file to your command and reads its result file. [command provider](command-provider.md).

## Authoring

`update`, `checksums`, `revbump`, `create`, and `edit` change a branch's working files and record an `Edit`; none of them commits.

- **`engine/update.go`, `engine/verbs.go`, `engine/stealth.go`** find the branch or start one, check the worktree out sparsely, and write the result into it.
- **`preparation`** turns an update into an edited tree: it materializes a disposable snapshot (`macports/workspace`, `scratch`), finds the release (`upstream`), and has `macports/portedit` make the edit.
- **`upstream`** finds a port's newest release from its forge's tags and releases (`forge/github`, `forge/gitlab`) or its livecheck, using the Portfile's own version rules. Versions are compared by MacPorts' `vercmp`, through the evaluator. **`outdated`** runs it for many ports at once, and `github` paces every request to GitHub's API across the process, below its documented secondary rate limit.
- **`macports/portedit`** makes an evaluated edit: `macports/portfile` changes literal values in the source text through `tcl/syntax` spans. `portedit/archives` fetches distfiles and computes checksums, and `macports/fidelity` checks that only what was meant to change did. `macports/dependency` regenerates Go and Rust dependency lists with `go2port` and `cargo2port`.
- **`macports/newport`** writes `create`'s first Portfile, marking what it guessed.
- **`engine/archivediff.go`** compares the old and new source archives for what a reviewer would ask about (`archive`).

## Shaping, submitting, and following

- **`engine/tidy.go`, `engine/tidyplan.go`** propose the commits a reviewer should see, one per port directory by default, and apply them without changing a file. Commit messages come from `macports/commitmsg`, and `macports/commitrules` checks them against what MacPorts asks.
- **`history`** makes `tidy`, `rebase` (`engine/verbs.go`), and `restore` complete transitions, which the engine's verbs decide and `history.Transitions` carries out. Each holds the branch's lock (`git.WithBranchLock`); a tidy or rebase records its checkpoint as prepared, makes its Git change, and settles the checkpoint, and the next of them on the branch finishes what a stopped one left. A rebase replays its commits with `git.Replay` before anything moves.
- **`engine/submit.go`, `engine/body.go`, `engine/forge.go`** push to your fork conditionally, never over someone else's push, and open or update the pull request with MacPorts' template filled in. GitHub access is `forge/github` over the shared client in `github`, with the login from `credential/keychain`, `GH_TOKEN`, or the GitHub CLI.
- **`engine/follow.go`** reads your pull requests' state, reviews, and CI. **`engine/review.go`** applies the commit rules to anyone's pull request. **`engine/clean.go`** removes what merged branches leave, and what checks whose process died left in providers (`LeftoverProvider`), under the check's lease.
- **`engine/status.go`** gathers each branch's status: its commits, edits, scope, latest check, and pull request.

## Serve and coordination

Several dockhand processes can share one database: a foreground `check`, a `serve`, a `watch`. `coord` keeps them straight. Each process has a session, identified by its pid and start time so a reused pid isn't mistaken for a live one. Work is held under fenced leases, and one `serve` leads while others stand by, taking over if the leader's session dies. Every change is written to an event journal that observers tail (Design v3 §11).

`engine/serve.go` runs the queue, people's checks first. Between checks it refreshes pull requests, looks for new releases once a day, and cleans up. `engine/servesubmit.go` opens pull requests for the updates serve prepared that passed, within §11's guardrails. `command/serve.go` posts the notifications and installs the launchd agent.

## On disk

| Where | What |
| --- | --- |
| `~/.dockhand/config.toml` (`$DOCKHAND_CONFIG`) | the configuration file, read by `internal/config` |
| `~/.dockhand/dockhand.db` (`--db`, `$DOCKHAND_DB`) | the records |
| `~/.dockhand/logs/check-N/` | each execution's logs, beside the database |
| `~/.dockhand/tart/` (`$DOCKHAND_TART_HOME`) | dockhand's Tart images, their golden copies, and the check clones |
| `~/.dockhand/ssh/` | the key the host uses to reach its guests |
| the user cache directory, `dockhand/indexes` (`$DOCKHAND_INDEX_CACHE`) | port indexes, keyed by source tree; disposable |
| one run root under the system temporary directory | a process's short-lived workspaces, removed when it exits (`scratch`) |
| `macports-branches` beside the clone | branch worktrees, unless `worktrees` says otherwise |

## Not in the binary

v2's packages were deleted on 2026-09-27: `workflow`, `state`, `publish`, `verify`, `git/changeset`, and `macports/dependents`. Commit `1cbcdf8b65` is the last with their code, and [v2](v2/README.md) keeps their documents. What v3 still used of them moved: `verify/staging` to `buildenv/staging`, and `assess` into `tools/survey`. `record`, v2's vocabulary, went last. The types v3 used moved to their owners, and the rest went with it:
- to `model`: a release and how it was chosen, an update's action and intent, and a release's scope;
- to `forge`: a pull request, its reference, state, and status;
- to `macports/commitmsg`: a commit's ticket references.

Everything else in the tree is in the binary or a tool, apart from `testsupport`, which tests share. `go list -deps ./cmd/dockhand` lists what the binary is made of.
