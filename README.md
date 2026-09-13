# Dockhand v2

Initial groundwork for `github.com/herbygillot/dockhand/v2`.

SQLite now holds workflow state behind `internal/state` contracts, with `internal/state/sqlite` as the implementation. One database can track multiple repositories; linked worktrees share an entry and separate clones remain distinct. Global `--db PATH` defaults to `$HOME/.dockhand/state.db`. The old lock-directory flags and Git ledger have been removed.

Request intake, read-only status, cancellation, and the single-target verification cycle are implemented. The cycle uses recorded claims and submission identities for capacity waiting, recovery, and cleanup. Explicit branch binding and native MacPorts evaluation are implemented through the workflow Go API. Tart now executes a real single-target verification against a prepared local VM image, with shared capacity, recovery, cancellation, and cleanup. `verify`, `wait`, `cancel`, and the current-process resident `start` command now use that cycle. Version- and revision-bump preparation are implemented, including bounded automatic GitHub version selection. Standalone publication of verified, committed contribution branches to GitHub is implemented. Unimplemented operations return explicit errors or recorded needs-attention outcomes.

- [Publication report](docs/activity/2026-09-13-publication.md)
- [Forge/upstream refactor report](docs/activity/2026-09-13-forge-upstream-boundaries.md)
- [Verification reuse report](docs/activity/2026-09-13-verification-reuse.md)
- [Working-tree verification report](docs/activity/2026-09-13-working-tree-verification.md)
- [Automatic version selection report](docs/activity/2026-09-13-automatic-version-selection.md)
- [Explicit version bump report](docs/activity/2026-09-13-explicit-version-bumps.md)
- [CLI execution and residency report](docs/activity/2026-09-12-cli-execution.md)
- [Tart execution report](docs/activity/2026-09-12-tart-execution.md)
- [Source binding and MacPorts evaluation report](docs/activity/2026-09-12-source-binding.md)
- [State-store design](docs/state.md)
- [SQLite implementation report](docs/activity/2026-09-12-sqlite-state.md)
- [SQLite performance measurements](docs/performance/2026-09-12-sqlite-state.md)
- [State design activity report](docs/activity/2026-09-12-state-design.md)
- [Component structure](docs/components.md)
- [Architecture](docs/architecture.md)
- [CLI design](docs/cli-design.md)
- [Principles](docs/principles.md)
- [Groundwork activity report](docs/activity/2026-09-10-groundwork.md)
- [Ledger implementation report](docs/activity/2026-09-10-ledger.md)
- [Lockfile simplification report](docs/activity/2026-09-11-lockfile.md)
- [Lock-directory report](docs/activity/2026-09-12-lock-directory.md)
- [Cobra integration report](docs/activity/2026-09-10-cobra.md)
- [Workflow intake and status report](docs/activity/2026-09-11-workflow-intake.md)
- [Verification cycle report](docs/activity/2026-09-11-verification-cycle.md)
- [Record package documentation report](docs/activity/2026-09-11-record-package.md)
- [Behavioral test report](docs/activity/2026-09-11-behavior-tests.md)
- [Testify conversion report](docs/activity/2026-09-12-testify.md)
- [Ledger and driver-cycle performance report](docs/performance/2026-09-12-performance-pass.md)

Run `make` (or `make build`) to build `./dh2`. Use `make test`, `make test-race`, and `make vet` for checks, and `make clean` to remove the binary. Override the output with `make BINARY=/path/to/dockhand` or the Go executable with `make GO=/path/to/go`. Tests cover workflow recovery, SQLite transactions, separate driver processes, repository isolation, CLI configuration, and Tcl syntax. Git is required by repository fixtures. MacPorts integration tests run when `port-tclsh` is available and otherwise skip; VM providers, credentials, and network access are not required. SQLite uses the pure-Go `modernc.org/sqlite` driver.

Writable service construction creates the selected database and its parent directory when needed. `dockhand status --json` reads recorded state without initializing missing state or contacting providers. Help, completion generation, and preparation previews do not open a database. Publication preflight reads recorded verification and initializes/migrates state through normal service construction. No config-directory setting or lock-file flag is present.

Cobra v1.10.2 supplies command help and shell completion; `usage` remains an alias for `help`. Verification submission, attachment, cancellation, and driver residency are wired. Other action handlers remain under construction. Use `workflow.Engine.BindVerification` to capture the current checkout or resolve an explicitly named local branch, inspect the evaluated metadata, then pass its returned request to `Submit`. Binding evaluates an isolated immutable tree; subports are explicitly selectable, and the initial evaluator requires the native MacPorts platform. Configure `tart.Provider` with the shared state store, repository, prepared local image, platform, and artifact directory. `DescribeEnvironment` returns the image digest to include in the accepted build configuration. The existing cycle consumes that job through the provider. `app.Build` supplies these dependencies and defaults artifacts to `artifacts/tart` beside the database.

The syntax package has `FuzzParse` and `FuzzSplitList` targets; their seed cases run in ordinary tests. `tools/stateperf` measures state writes and driver cycles against increasing history sizes and concurrent processes. Historical Git-ledger measurements remain under `docs/performance`; the former executable harness is available in commit `ec812d2`.

The opt-in real VM acceptance test requires macOS with a GUI login domain, Tart, a prepared local image with the Tart guest agent, passwordless guest sudo, MacPorts with Tcl JSON support, and no installed ports. It creates a disposable clone and preserves host diagnostics. It proves that one driver process can submit and exit and another process can settle and release the same run:

```sh
DOCKHAND_TEST_TART_IMAGE=dockhand-base-tahoe \
go test -v ./internal/verify/tart -run '^TestRealTartBuildSurvivesSubmittingDriverExit$' -timeout 16m
```

All cooperating drivers using the same Tart home must use the same DB, capacity, and artifact directory. A separate DB does not coordinate that shared pool. Base images are hashed by contents; the first hash in each process can be expensive. Provisioning base images remains later work. `verify --image` selects the image; an optional `--capacity` establishes the shared pool limit. Subsequent `wait` and `start` invocations use the accepted job settings and recorded pool limit.

Verify current edits or committed branch contents, then reattach by the printed job ID:

```sh
dockhand verify jq --image dockhand-base-tahoe --wait
dockhand verify jq --branch update-jq --image dockhand-base-tahoe
dockhand wait <job_id> --trace
# Or submit and stay attached in one invocation:
dockhand verify jq --branch update-jq --image dockhand-base-tahoe --wait

dockhand cancel <job_id> --wait
dockhand start
```

Omitting `--branch` captures tracked working-tree contents, including staged additions and deletions, without changing the index or branch. New files must be staged; explicit `--branch` uses committed contents. The accepted snapshot stays fixed while editing continues; each invocation selects one port directory or unique directory name, optionally `--subport` and repeated `--variant` choices. This first CLI uses job IDs for wait/cancel. General selectors and branch-only port inference remain later work. A tracked contribution retains its edited targets; verification may select a different target without redefining that contribution.

Matching passing verification is reused when the complete source tree, target, variants, image, verifier implementation, and build settings agree. You can verify edits, commit the same contents, and verify that branch without another build. Status cites the original attempt. Use `verify --fresh` to require a new execution; reattaching with `wait` preserves the existing decision. Older results without a recorded verifier identity require a fresh build before they can be reused.

Without `--wait`, verification remains attached while capacity is unavailable and returns at admission or a conclusive outcome. `--wait` and `--trace` follow completion. Ctrl-C detaches without canceling accepted work; `start` runs until interrupted and must be invoked separately for each repository. If nobody is running cycles for an admitted job, its VM can continue and occupy capacity until a later cycle collects its outcome. `wait` resumes a fixed job; it never submits another verification. JSON results go to stdout, progress and trace output to stderr. Exit codes are 0 for the requested milestone, 2 for failed work, 3 for needs-attention, 130 for interruption/canceled work, and 1 for other errors. Confirmed cancellation is successful for `cancel --wait`.

Preview or prepare a version update from committed source:

```sh
dockhand bump jq --diff
dockhand bump jq --no-verify
dockhand bump jq --image dockhand-base-tahoe --wait
dockhand bump jq 1.8.1 --diff
```

Omitting the version selects the newest eligible stable numeric GitHub version using supported evaluated livecheck metadata and native MacPorts ordering. Already-current ports complete without creating a branch or starting verification. Unknown or incomplete discovery requires attention. Explicit versions also support the evaluated upstream tag prefix. The first editor handles supported literal GitHub version sources and one direct archive with literal checksums; see the [CLI design](docs/cli-design.md) for limits. Verification uses available dependency binaries by default; `--from-source` opts into building the dependency stack from source.

Publish a tracked contribution after verifying and committing its contents:

```sh
dockhand publish --branch update-jq --dry-run
dockhand publish --branch update-jq --wait
```

Without `--branch`, publication selects the current local branch's committed contents. The first path requires one contribution commit, changes confined to its one tracked port directory, and passing evidence for its complete tree and target. It uses that result's recorded image, verifier, platform, variants, and build settings; no image flag or new build is needed. Missing or failed evidence requires an explicit `verify` first.

The push remote defaults to `origin`; the PR target comes from `upstream` when configured, then the fork parent, then the push repository. `--remote`, `--upstream`, and `--base` override those choices. Git uses its credentials; the API reads `GH_TOKEN`, then `GITHUB_TOKEN`. Tokens are not persisted. The commit supplies the title and initial body; existing PR bodies are preserved.

Publication without `--wait` returns after driver pickup or an earlier terminal outcome. `--wait` follows remote confirmation. Resume accepted work using its job ID with `wait`, or run `start`; Ctrl-C detaches. A lost PR response is reconciled by observation without repeating the write. If the outcome cannot be established, the job stays pending and reserves that remote branch. Cancellation cannot undo an already issued PR request. Combined `bump --publish`, missing-verification scheduling, rebase/amend commands, and post-publication monitoring remain future work.
