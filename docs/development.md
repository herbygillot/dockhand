# Development and implementation notes

This document collects build, testing, and implementation details previously kept in the introductory README.

## Build and test

Run `make` (or `make build`) to build `./dockhand`. Use `make test`, `make test-race`, and `make vet` for checks, and `make clean` to remove the binary. Override the output with `make BINARY=/path/to/dockhand` or the Go executable with `make GO=/path/to/go`. Tests cover workflow recovery, SQLite transactions, separate driver processes, repository isolation, CLI configuration, and Tcl syntax. Git is required by repository fixtures. MacPorts integration tests run when `port-tclsh` is available and otherwise skip; VM providers, credentials, and network access are not required. SQLite uses the pure-Go `modernc.org/sqlite` driver.

The module requires Go 1.27.1 or newer. Dependencies are vendored: the `vendor` directory holds every module the build uses, so a build needs no module download and no network, and `go build`, `go test`, and `go vet` use it automatically. After changing a dependency in `go.mod`, run `go mod tidy && go mod vendor` and commit the result; a `vendor/modules.txt` that disagrees with `go.mod` fails the build. The `deadcode` target runs a tool by version through `go run`, which is unaffected by vendoring.

The syntax package has `FuzzParse` and `FuzzSplitList` targets; their seed cases run in ordinary tests.

## Real VM acceptance test

The opt-in real VM acceptance test requires macOS with a GUI login domain, Tart, a prepared local image with the Tart guest agent, passwordless guest sudo, MacPorts with Tcl JSON support, and no installed ports. It creates a disposable clone and preserves host diagnostics. It proves that one driver process can submit and exit and another process can settle and release the same run:

```sh
DOCKHAND_TEST_TART_IMAGE=dockhand-base-tahoe \
go test -v ./internal/verify/tart -run '^TestRealTartBuildSurvivesSubmittingDriverExit$' -timeout 16m
```

## State and service boundaries

SQLite now holds workflow state behind `internal/state` contracts, with `internal/state/sqlite` as the implementation. One database can track multiple repositories; linked worktrees share an entry and separate clones remain distinct. Global `--db PATH` defaults to `$HOME/.dockhand/state.db`. The old lock-directory flags and Git ledger have been removed.

Writable service construction creates the selected database and its parent directory when needed. `dockhand status [job_id]` reads recorded state without initializing missing state or contacting providers. Use `--active` for queued/active work, `--branch <branch>` for a recorded contribution, and `--json` for structured output. `--active` may combine with either selector. Help, completion generation, and preparation previews do not open a database. Publication preflight reads recorded verification and initializes/migrates state through normal service construction. No config-directory setting or lock-file flag is present.

Cobra v1.10.2 supplies command help and shell completion; `usage` remains an alias for `help`. Verification submission, attachment, cancellation, and driver residency are wired. Other action handlers remain under construction. Use `workflow.Engine.BindVerification` to capture the current checkout or resolve an explicitly named local branch, inspect the evaluated metadata, then pass its returned request to `Submit`. Binding evaluates an isolated immutable tree; subports are explicitly selectable, and the initial evaluator requires the native MacPorts platform. Configure `tart.Provider` with the shared state store, repository, prepared local image, platform, and artifact directory. `DescribeEnvironment` returns the image digest to include in the accepted build configuration. The existing cycle consumes that job through the provider. `app.Build` supplies these dependencies and defaults artifacts to `artifacts/tart` beside the database.

## Implementation status

Version and revision preparation, native MacPorts source evaluation, Tart verification, GitHub publication, authentication, and image setup are implemented. The workflow scheduler supports multiple isolated verification attempts, and `macports/dependents` discovers candidate downstream coverage against frozen source. Connecting discovery to durable verification planning remains work in progress. Automatic version selection supports eligible GitHub and GitLab sources. Unimplemented operations return explicit errors or recorded needs-attention outcomes.
