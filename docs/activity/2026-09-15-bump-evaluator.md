# Bump planner: evaluator boundary

Started the approved preparation redesign by separating native MacPorts evaluation from its shared contracts. This is the first prerequisite; broader Portfile preparation support is not enabled by this change.

## Changes

- Moved interpreter startup, compatibility checks, metadata decoding, selector resolution, fetch observations, version comparison, and embedded Tcl scripts into `internal/macports/eval`.
- Kept source-bound `Tree`/`Context`, metadata snapshots, runtime observations, selection values, and reader contracts in `internal/macports`.
- Added `NativeReader` for assessment/discovery, avoiding concrete evaluator dependencies in those capabilities. Application wiring constructs `eval.Evaluator`; production capability and workflow packages do not import it.
- Shared name/selector validation stays with the source contracts. Public constructors and existing metadata semantics remain available without aliases back to the native evaluator.
- Moved evaluator tests with the implementation and kept the pure snapshot/toolchain policy test with the shared metadata contract.
- Added the bump-planner design and updated the component map and roadmap with scope, preservation fixtures, sequencing, and survey evidence. Modeled contexts are planned as an explicit isolated operation; ordinary `Reader.Evaluate` still requires native evaluation.

## Code provenance

Moved and adapted current v2 code; no v1 code, comments, or tests were copied. New work consists of the `NativeReader` contract, the package ownership/validation boundary, caller wiring, and design/package documentation. The four embedded Tcl scripts are byte-for-byte unchanged.

## Validation

- `go test -p 1 ./...`: passed in the isolated migration copy, with localhost listeners permitted for existing HTTP integration tests.
- `go vet ./...`: passed.
- Built the CLI and compared JSON from `assess jq deno gh terraform helm` against the pre-extraction survey baseline: identical.
- Checked that only application wiring imports the concrete evaluator in production code.
- Checked the complete patch against the clean tracked checkout before applying it; unrelated untracked review documents were preserved.

No new dependency or CLI option was introduced. The next implementation step is declaration/checksum ownership and artifact planning, followed by scoped edits and explicit-version preparation independent of forge discovery. Initial complete-preparation targets remain existing Terraform/Helm series, Deno, and gh; coordinated Rust maintenance remains excluded.
