# Dockhand v2

Initial groundwork for `github.com/herbygillot/dockhand/v2`.

The package tree compiles, but the CLI workflows are not operational. Git-backed ledger persistence is implemented. Driver-cycle execution, MacPorts evaluation, verification, publication, and CLI wiring remain unfinished. Unimplemented operations return explicit errors.

- [Component structure](docs/components.md)
- [Architecture](docs/architecture.md)
- [CLI design](docs/cli-design.md)
- [Principles](docs/principles.md)
- [Groundwork activity report](docs/activity/2026-09-10-groundwork.md)
- [Ledger implementation report](docs/activity/2026-09-10-ledger.md)

Compile the packages with `go build ./...`. No test suite has been imported or added; the ledger report describes runtime checks performed in disposable repositories.
