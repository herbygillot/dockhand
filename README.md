# Dockhand v2

Initial groundwork for `github.com/herbygillot/dockhand/v2`.

The package tree compiles. Git-backed ledger persistence, workflow request acceptance, and snapshot status reporting are implemented. `dockhand status` supports human and JSON output. Cobra command parsing and help, the global `--lockfile` / `-L` option, ledger initialization, and service construction are wired. A workflow cycle can now verify one target against an existing committed revision through an injected provider, including recovery, cancellation, and cleanup. Real provider execution, MacPorts evaluation, publication, and action command execution remain unfinished. Unimplemented operations return explicit errors.

- [Component structure](docs/components.md)
- [Architecture](docs/architecture.md)
- [CLI design](docs/cli-design.md)
- [Principles](docs/principles.md)
- [Groundwork activity report](docs/activity/2026-09-10-groundwork.md)
- [Ledger implementation report](docs/activity/2026-09-10-ledger.md)
- [Lockfile simplification report](docs/activity/2026-09-11-lockfile.md)
- [Cobra integration report](docs/activity/2026-09-10-cobra.md)
- [Workflow intake and status report](docs/activity/2026-09-11-workflow-intake.md)
- [Verification cycle report](docs/activity/2026-09-11-verification-cycle.md)
- [Record package documentation report](docs/activity/2026-09-11-record-package.md)

Compile the packages with `go build ./...`. No test suite has been imported or added; the activity reports describe temporary validation performed in disposable repositories.

The ledger writer lock defaults to `$HOME/.dockhand/ledger.lock`; `--lockfile PATH` or `-L PATH` selects another path. The ledger creates the file and any missing parent directories when initialized, without acquiring the lock. There is no config-directory setting. Help and completion work outside a Git repository and create no directories or files.

The CLI uses Cobra v1.10.2, matching Dockhand v1. Run `dockhand help <command>` for command-specific help; `usage` is an alias for `help`. Cobra supplies shell completion generation through `dockhand completion`. Use `dockhand status --json` for the typed ledger projection. Action commands remain unfinished; the workflow submission API accepts queued work, and `workflow.Engine.Cycle` advances the implemented verification slice. CLI action submission and driver residency are not wired yet.
