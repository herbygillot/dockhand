# Dockhand v2

Initial groundwork for `github.com/herbygillot/dockhand/v2`.

SQLite now holds workflow state behind `internal/state` contracts, with `internal/state/sqlite` as the implementation. One database can track multiple repositories; linked worktrees share an entry and separate clones remain distinct. Global `--db PATH` defaults to `$HOME/.dockhand/state.db`. The old lock-directory flags and Git ledger have been removed.

Request intake, read-only status, cancellation, and the single-target verification cycle are implemented. The cycle uses recorded claims and submission identities for capacity waiting, recovery, and cleanup. Explicit branch binding and native MacPorts evaluation are implemented through the workflow Go API. Tart now executes a real single-target verification against a prepared local VM image, with shared capacity, recovery, cancellation, and cleanup. `verify`, `wait`, `cancel`, and the current-process resident `start` command now use that cycle. Preparation and publication remain unfinished. Unimplemented operations return explicit errors or recorded needs-attention outcomes.

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

Compile with `go build ./...`. Run `go test ./...`, `go test -race ./...`, and `go vet ./...`. Tests cover workflow recovery, SQLite transactions, separate driver processes, repository isolation, CLI configuration, and Tcl syntax. Git is required by repository fixtures. MacPorts integration tests run when `port-tclsh` is available and otherwise skip; VM providers, credentials, and network access are not required. SQLite uses the pure-Go `modernc.org/sqlite` driver.

Writable service construction creates the selected database and its parent directory when needed. `dockhand status --json` reads recorded state without initializing missing state or contacting providers. Help, completion generation, and previews do not open a database. No config-directory setting or lock-file flag is present.

Cobra v1.10.2 supplies command help and shell completion; `usage` remains an alias for `help`. Verification submission, attachment, cancellation, and driver residency are wired. Other action handlers remain under construction. Use `workflow.Engine.BindVerification` to resolve a literal local branch and port directory, inspect the evaluated metadata, then pass its returned request to `Submit`. Binding uses an isolated copy of committed contents; subports are explicitly selectable, and the initial evaluator requires the native MacPorts platform. Configure `tart.Provider` with the shared state store, repository, prepared local image, platform, and artifact directory. `DescribeEnvironment` returns the image digest to include in the accepted build configuration. The existing cycle consumes that job through the provider. `app.Build` supplies these dependencies and defaults artifacts to `artifacts/tart` beside the database.

The syntax package has `FuzzParse` and `FuzzSplitList` targets; their seed cases run in ordinary tests. `tools/stateperf` measures state writes and driver cycles against increasing history sizes and concurrent processes. Historical Git-ledger measurements remain under `docs/performance`; the former executable harness is available in commit `ec812d2`.

The opt-in real VM acceptance test requires macOS with a GUI login domain, Tart, a prepared local image with the Tart guest agent, passwordless guest sudo, MacPorts with Tcl JSON support, and no installed ports. It creates a disposable clone and preserves host diagnostics. It proves that one driver process can submit and exit and another process can settle and release the same run:

```sh
DOCKHAND_TEST_TART_IMAGE=dockhand-base-tahoe \
go test -v ./internal/verify/tart -run '^TestRealTartBuildSurvivesSubmittingDriverExit$' -timeout 16m
```

All cooperating drivers using the same Tart home must use the same DB, capacity, and artifact directory. A separate DB does not coordinate that shared pool. Base images are hashed by contents; the first hash in each process can be expensive. Provisioning base images remains later work. `verify --image` selects the image; an optional `--capacity` establishes the shared pool limit. Subsequent `wait` and `start` invocations use the accepted job settings and recorded pool limit.

Verify one port from a committed local branch, then reattach by its printed job ID:

```sh
dockhand verify jq --branch update-jq --image dockhand-base-tahoe
dockhand wait <job_id> --trace
# Or submit and stay attached in one invocation:
dockhand verify jq --branch update-jq --image dockhand-base-tahoe --wait

dockhand cancel <job_id> --wait
dockhand start
```

Omitting `--branch` selects the current local branch. Only committed contents are included; each invocation selects one port directory or unique directory name, optionally `--subport` and repeated `--variant` choices. This first CLI uses job IDs for wait/cancel. General selectors, branch-only port inference, and working-tree adoption remain later work. A tracked branch is currently associated with its accepted target; verifying unrelated targets on that branch is rejected.

Without `--wait`, verification remains attached while capacity is unavailable and returns at admission or a conclusive outcome. `--wait` and `--trace` follow completion. Ctrl-C detaches without canceling accepted work; `start` runs until interrupted and must be invoked separately for each repository. If nobody is running cycles for an admitted job, its VM can continue and occupy capacity until a later cycle collects its outcome. `wait` resumes a fixed job; it never submits another verification. JSON results go to stdout, progress and trace output to stderr. Exit codes are 0 for the requested milestone, 2 for failed work, 3 for needs-attention, 130 for interruption/canceled work, and 1 for other errors. Confirmed cancellation is successful for `cancel --wait`.
