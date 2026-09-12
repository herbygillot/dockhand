# Dockhand v2

Initial groundwork for `github.com/herbygillot/dockhand/v2`.

The approved [state-store design](docs/state.md) replaces Git-ledger persistence with `internal/state` contracts and a SQLite backend. It adds multiple repositories in one database and replaces lock-directory selection with `--db`, defaulting to `$HOME/.dockhand/state.db`. This migration is not implemented yet; the status below describes the current code.

The package tree compiles. Git-backed ledger persistence, workflow request acceptance, and snapshot status reporting are implemented. `dockhand status` supports human and JSON output. Cobra command parsing and help, the global `--lock-dir` / `-L` option, ledger initialization, and service construction are wired. A workflow cycle can now verify one target against an existing committed revision through an injected provider, including recovery, cancellation, and cleanup. Real provider execution, MacPorts evaluation, publication, and action command execution remain unfinished. Unimplemented operations return explicit errors.

- [State-store design](docs/state.md)
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

Compile the packages with `go build ./...`. Run the behavioral tests with `go test ./...`, or include concurrency checking with `go test -race ./...`. The suite covers workflow behavior, ledger durability, resource locking, configuration wiring, and Tcl syntax, using Testify assertions. Resource-lock tests include a separate holder process and recovery after its termination; app tests cover shared worktrees and independent repositories. Git must be available on `PATH`, with support for `show-ref --exists` and SHA-256 repositories; the ledger tests exercise both Git object formats. Git fixtures and lockfiles live in temporary directories. No MacPorts installation, Tcl interpreter, VM provider, credentials, or network access is required by the tests.

The syntax package also has two fuzz targets. Run either with `go test ./internal/tcl/syntax -run '^$' -fuzz '^FuzzParse$' -fuzztime=20s` or substitute `FuzzSplitList`. Their seed cases run during ordinary `go test` executions.

Until the SQLite migration, the current CLI uses `--lock-dir PATH` / `-L PATH`, defaulting to `$HOME/.dockhand/lock`. Services create a writer lock keyed by the canonical Git common directory, so linked worktrees coordinate. All processes working on the same resource must select the same lock directory. There is no config-directory setting. Help and completion work outside a Git repository and create no directories or files. See the [historical lock-directory report](docs/activity/2026-09-12-lock-directory.md) for details.

The CLI uses Cobra v1.10.2, matching Dockhand v1. Run `dockhand help <command>` for command-specific help; `usage` is an alias for `help`. Cobra supplies shell completion generation through `dockhand completion`. Use `dockhand status --json` for the typed ledger projection. Action commands remain unfinished; the workflow submission API accepts queued work, and `workflow.Engine.Cycle` advances the implemented verification slice. CLI action submission and driver residency are not wired yet.
