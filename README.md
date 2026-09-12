# Dockhand v2

Initial groundwork for `github.com/herbygillot/dockhand/v2`.

SQLite now holds workflow state behind `internal/state` contracts, with `internal/state/sqlite` as the implementation. One database can track multiple repositories; linked worktrees share an entry and separate clones remain distinct. Global `--db PATH` defaults to `$HOME/.dockhand/state.db`. The old lock-directory flags and Git ledger have been removed.

Request intake, read-only status, cancellation, and the single-target verification cycle are implemented. The cycle uses recorded claims and submission identities for capacity waiting, recovery, and cleanup. Real provider execution, MacPorts evaluation, preparation, publication, action-command execution, and persistent residency remain unfinished. Unimplemented operations return explicit errors or recorded needs-attention outcomes.

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

Compile with `go build ./...`. Run `go test ./...`, `go test -race ./...`, and `go vet ./...`. Tests cover workflow recovery, SQLite transactions, separate driver processes, repository isolation, CLI configuration, and Tcl syntax. Git is required by repository fixtures; no MacPorts installation, VM provider, credentials, or network access is required by tests. SQLite uses the pure-Go `modernc.org/sqlite` driver.

Writable service construction creates the selected database and its parent directory when needed. `dockhand status --json` reads recorded state without initializing missing state or contacting providers. Help, completion generation, and previews do not open a database. No config-directory setting or lock-file flag is present.

Cobra v1.10.2 supplies command help and shell completion; `usage` remains an alias for `help`. Action submission and driver residency are not wired yet. Use the workflow Go API to exercise the implemented verification path.

The syntax package has `FuzzParse` and `FuzzSplitList` targets; their seed cases run in ordinary tests. `tools/stateperf` measures state writes and driver cycles against increasing history sizes and concurrent processes. Historical Git-ledger measurements remain under `docs/performance`; the former executable harness is available in commit `ec812d2`.
