# Groundwork activity report — September 10, 2026

This pass establishes code organization from `docs/components.md`. It does not implement an end-to-end bump, verification, or publication workflow. The existing module remains `github.com/herbygillot/dockhand/v2`, with Go 1.27.1 and no added external Go dependencies.

## Organization and boundaries

Created the executable entry point and the `app`, `cli`, `proc`, `model`, `ledger`, `workflow`, `prepare`, `upstream`, `verify`, `verify/tart`, `publish`, `macports`, `git`, and `forge/github` components. Added the `text` leaf package to support Tcl syntax and precise edits, and retained the Tcl shell, RPC, and syntax subpackages.

The new records distinguish logical changes, immutable source revisions, requested jobs, concrete attempts, verification plans, resources, publication actions, and persistent PR associations. Waiting, tracing, and JSON preferences stay in the CLI. Concrete build inputs are separate from planned dependencies on future artifacts. Preparation edits use ports-tree-relative paths and per-file preconditions, allowing shared PortGroup edits.

The workflow owns intake and advancement. `proc` only models driver discovery and lifetime. Requests are intended to pass through ledger transactions; no socket or alternate submission transport was introduced. Storage depends on model and Git mechanics. Preparation, verification, and publication return results rather than importing or mutating the ledger. GitHub and Tart adapters satisfy their consumer interfaces but do not perform external operations yet.

## Reused from v1

Source: `~/Source/project-dockhand`, commit `1710985dbada8e706132a8deea6031df9e545126`. These ten files were imported to the same relative paths:

- `internal/text/text.go`
- `internal/text/edit.go`
- `internal/tcl/syntax/error.go`
- `internal/tcl/syntax/list.go`
- `internal/tcl/syntax/parse.go`
- `internal/tcl/syntax/types.go`
- `internal/tcl/syntax/visit.go`
- `internal/tcl/shell/proc.go`
- `internal/tcl/rpc/session.go`
- `internal/tcl/rpc/loop.tcl`

The nine Go files were parsed without comments and reformatted. Imports now use the v2 module path. Prose comments, lint annotations, and other old comment-only conventions were discarded. The required `go:embed loop.tcl` compiler directive was reintroduced. Comment lines were removed from the Tcl loop; Tcl code such as `uplevel #0` was preserved.

A token comparison confirmed that imported Go code otherwise matches v1. No source-level behavior changes were made to these imported helpers. New, short package comments were authored separately in the four `doc.go` files listed below. No v1 test files, test helpers, fixtures, vendored dependencies, or old operational orchestration were imported.

## Newly authored code

All Go files below were written for this groundwork pass:

- `cmd/dockhand/main.go`
- `internal/app/app.go`
- `internal/cli/cli.go`
- `internal/forge/github/client.go`
- `internal/git/refs.go`
- `internal/git/repository.go`
- `internal/ledger/codec.go`
- `internal/ledger/state.go`
- `internal/ledger/store.go`
- `internal/macports/context.go`
- `internal/macports/evaluator.go`
- `internal/model/attempt.go`
- `internal/model/change.go`
- `internal/model/identity.go`
- `internal/model/job.go`
- `internal/model/publication.go`
- `internal/model/resource.go`
- `internal/model/source.go`
- `internal/model/verification.go`
- `internal/prepare/prepare.go`
- `internal/proc/manager.go`
- `internal/publish/publish.go`
- `internal/tcl/rpc/doc.go`
- `internal/tcl/shell/doc.go`
- `internal/tcl/syntax/doc.go`
- `internal/text/doc.go`
- `internal/upstream/discovery.go`
- `internal/verify/plan.go`
- `internal/verify/provider.go`
- `internal/verify/tart/provider.go`
- `internal/workflow/cycle.go`
- `internal/workflow/engine.go`
- `internal/workflow/status.go`
- `internal/workflow/submit.go`

The new code comprises the domain records and identifiers; Git repository discovery and read helpers; the draft ledger document codec and transactional store boundary; a bound MacPorts source-context value; preparation, discovery, verification, and publication contracts; workflow intake/cycle/status signatures; process management and integration skeletons; configuration/composition types; and a minimal CLI entry point. The Git command/environment handling was rewritten with inspiration from v1's repository isolation approach, rather than copying its repository implementation.

The codec and context constructors contain basic implementation. This is not a completed or hardened persistence format. Identifiers, record validation, defensive copies, request deduplication, claims, and other operational invariants still need deliberate implementation as the APIs settle.

The ledger's `Update` method currently returns an error without invoking its callback. Workflow submission returns an error without a receipt. Provider and forge methods return errors without contacting external services. No placeholder reports a completed build, successful publication, or durable acceptance.

## Deliberately unfinished

- Git-backed ledger reading/writing, atomic ref updates, locking, source pins, and note projection.
- Durable request intake, cancellation/review application, scheduling, claims, recovery, and status projection.
- Driver discovery, startup, resident/temporary loops, and CLI waiting/trace behavior.
- MacPorts initialization, evaluation workers, selector resolution, and context materialization.
- Upstream release discovery and assessment; version/revision/checksum edits and fidelity checks.
- Verification planning, dependent impact analysis, verdict logic, and actual Tart provisioning/builds.
- Publication policy, Git pushes, forge calls, and uncertain-action reconciliation.
- Configuration loading, application wiring, command parsing, and human/JSON result rendering.

The executable only provides a groundwork help message. Ordinary commands return an explicit not-implemented error. Phase-two actions have room in the vocabulary, but their handlers are not implemented. This pass neither builds ports nor changes a ports tree, starts VMs or background drivers, creates PRs, or initializes a Git repository for v2.

## Validation

- Formatted all Go files with `gofmt`.
- `go build ./...` passed for the staged source tree.
- Inspected the Go import graph: the intended dependency direction holds and there are no cycles.
- Verified imported Go tokens against v1, allowing the module-path update; checked that inherited prose comments were removed.
- Confirmed no test files, test fixtures, or v1 module imports were included.

No tests were copied, authored, or run. Compilation and source checks do not establish runtime correctness. Future tests should be designed intentionally around the v2 contracts.

Inventory: 43 Go files (34 newly authored and 9 imported), plus the imported Tcl loop.

## Follow-up: invocation-owned driver execution

Removed `Config.DriverExecutable`, `proc.Manager.Executable`, and `proc.Manager.Ensure`. See [driver execution](../architecture.md#driver-execution) for the current model; it supersedes the earlier startup references in this report.
