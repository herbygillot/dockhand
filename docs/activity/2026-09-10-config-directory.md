# Startup configuration and lockfile report — September 10, 2026

Implemented startup config-directory creation and configurable ledger lockfile selection. The default config directory is `$HOME/.dockhand`, overridden by `DOCKHAND_CONFIG_DIR`. The lock defaults to `ledger.lock` inside that directory and can be overridden with the global `--lockfile` option.

## Organization and behavior

`app.Config` now carries `ConfigDir` and `Lockfile`. The new `app.Initialize` resolves paths and creates the config directory. An explicit config directory supplied by an embedded caller takes precedence over the environment. Empty or unset `DOCKHAND_CONFIG_DIR` uses the home-directory default. Relative paths resolve against the current working directory. New directories use mode 0700; existing directory permissions are preserved.

`cli.ParseGlobal` recognizes `--lockfile PATH`, `--lockfile=PATH`, and help flags. Global options work on either side of the command until `--`; empty lockfile values are errors, and repeated overrides use the last value. Command arguments remain available to the command execution layer. The existing command-specific options remain unimplemented.

`cli.Run` now receives an initial `app.Config`, applies the parsed override, and initializes startup configuration. Help, usage, and an invocation without a command display help and resolved paths without opening a Git repository. Other invocations construct the services before reaching the existing command-execution stub.

`app.Build` now constructs the existing service objects around the opened Git repository and configured ledger. It supplies `ledger.Options.Lockfile`, connects the shared preparation/discovery services, and constructs provider/forge adapters without calling them. Workflow command execution and the adapters' operational methods remain unimplemented; service construction does not report a successful job or contact a provider.

`ledger.New` requires an explicit absolute lockfile path. The ledger no longer chooses a path or reads environment variables. A write creates missing lockfile parent directories with mode 0700, then opens the configured lockfile with mode 0600 and uses the existing bounded `flock` acquisition. Reads and construction create neither the lockfile nor its parent directories. The config directory is created separately at startup.

The default lock now coordinates writers across repositories using the same config directory. A supplied custom lockfile changes that coordination scope. Git's guarded ref updates remain in place, and ledger data stays in the ports repository. Nothing copies, deletes, or reuses the former common-directory lockfile automatically.

## Newly authored and modified code

All code in this pass was newly authored or adapted from the existing v2 scaffold. No v1 code, comments, or tests were copied. No external dependencies were added.

New files:

- `internal/app/config.go`
- `internal/cli/global.go`

Modified files:

- `cmd/dockhand/main.go`
- `internal/app/app.go`
- `internal/cli/cli.go`
- `internal/ledger/store.go`
- `internal/ledger/lock.go`

Updated the README, architecture, component map, and CLI design. Added a follow-up link to the earlier ledger activity report so its historical lockfile description is not mistaken for the current API.

## Validation

Formatting, `go build ./...`, and `go vet ./...` passed. A temporary integration harness ran with the race detector against disposable directories and Git repositories. It exercised:

- Home-directory defaults, environment overrides, explicit configuration, CLI precedence, relative paths, and paths containing spaces.
- Startup directory creation and permissions; help outside a repository; startup with an explicit config directory and no home environment value.
- Global flags before and after commands, equals syntax, repeated overrides, `--`, and missing/empty argument errors.
- Passing the configured path from application construction into actual ledger writes, with no legacy common-directory lockfile created.
- Rejection of missing or relative paths at the store boundary; reads without creating lockfiles or custom parent directories.
- Contention between separate repositories using the same lockfile, timeout behavior, reads while the lock is held, and independent progress with a different configured lockfile.
- Invalid config-directory paths and explicit not-implemented results for command workflows.

The harness and compiled binary were kept outside the repository. No tests were installed. Validation used temporary config directories and did not modify the user's config directory, ports tree, or providers.

## Follow-up: Cobra command parsing

Cobra now owns command and flag parsing. The hand-written `ParseGlobal` implementation was removed; startup configuration, the default lockfile, and explicit path validation are preserved. See the [Cobra integration report](2026-09-10-cobra.md) for the current command-layer structure.

## Follow-up: lockfile simplification

The config-directory setting, environment override, and startup directory creation have been removed. `--lockfile` now has the short alias `-L`, with the default `$HOME/.dockhand/ledger.lock` owned by Cobra. Ledger construction creates the lockfile and missing parent directories; help and completion create nothing. See the [lockfile simplification report](2026-09-11-lockfile.md) for the current behavior.
