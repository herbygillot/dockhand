# Dockhand v2

Initial groundwork for `github.com/herbygillot/dockhand/v2`.

The package tree compiles, but the CLI workflows are not operational. Git-backed ledger persistence is implemented. Cobra command parsing and help, the global `--lockfile` / `-L` option, ledger initialization, and service construction are wired. Driver-cycle execution, MacPorts evaluation, verification, publication, and command execution remain unfinished. Unimplemented operations return explicit errors.

- [Component structure](docs/components.md)
- [Architecture](docs/architecture.md)
- [CLI design](docs/cli-design.md)
- [Principles](docs/principles.md)
- [Groundwork activity report](docs/activity/2026-09-10-groundwork.md)
- [Ledger implementation report](docs/activity/2026-09-10-ledger.md)
- [Lockfile simplification report](docs/activity/2026-09-11-lockfile.md)
- [Cobra integration report](docs/activity/2026-09-10-cobra.md)

Compile the packages with `go build ./...`. No test suite has been imported or added; the ledger report describes runtime checks performed in disposable repositories.

The ledger writer lock defaults to `$HOME/.dockhand/ledger.lock`; `--lockfile PATH` or `-L PATH` selects another path. The ledger creates the file and any missing parent directories when initialized, without acquiring the lock. There is no config-directory setting. Help and completion work outside a Git repository and create no directories or files.

The CLI uses Cobra v1.10.2, matching Dockhand v1. Run `dockhand help <command>` for command-specific help; `usage` is an alias for `help`. Cobra supplies shell completion generation through `dockhand completion`. Workflow commands and JSON result rendering remain unfinished.
