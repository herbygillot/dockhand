# Ledger and driver-cycle performance pass — 2026-09-12

## Implementation

- Added `git.Repository.CommitTrees`: one batch lookup validates commit types and resolves their root trees. Immutable IDs are required; failed or malformed responses return no partial result. The ledger retains object-type validation, commit/tree agreement checks, pin repair, and guarded publication of all required refs.
- Shared snapshot capture and scope validation between workflow status and cycles without changing the public status projection. Cycles use raw snapshots and group selected attempts once for candidate selection.
- Added workflow eligibility predicates for controls, jobs, and cleanup. Settled work, live claims, future retries, active resources, and unexpired retention do not open write transactions. Cancellation before admission can bypass a retry delay; missing or invalid cleanup ownership remains diagnosable.
- Handlers still reread current state and claim work under the writer lock. Snapshot selection cannot authorize an external action. Cleanup remains independent of job completion, and snapshots refresh after handlers so completion can trigger cleanup in the same pass.
- Provider capabilities are obtained lazily for candidate actions. Idle cycles need one snapshot read, no writer acquisition, and no provider call.
- Added an OS acquisition gate per resource lock after the new concurrent-driver stress case exposed starvation from rapid writer reacquisition. A queued gate holder reserves the next resource handoff; timeouts stay unchanged. Gate contenders are not promised FIFO order.
- Extended the performance harness to run concurrent driver subprocesses as well as ledger writers/readers. Raw cycle samples distinguish successful passes from actual advancement.

## Provenance and scope

All new code and tests were authored for v2; existing v2 scope-validation code was reorganized. No v1 code, comments, or tests were copied. No dependencies, durable record shapes, transaction callback semantics, or lock timeouts changed. The preceding lock-directory work remains in the working tree.

Human-edit adoption and the commands surrounding it are deliberately left for a separate user-facing workflow design discussion. This pass adds no command or adoption behavior for human edits.

## Validation

`go test -race ./...` and `go vet ./...` passed for the production changes. New tests cover both Git object formats, one-process batch lookup, missing/wrong/malformed source results, idle cycles while another holder owns the writer lock, queued cancellation, partial controls, stale candidate selection, and accepted updates from three concurrent writer processes. Existing stale-claim, submission reconciliation, cancellation, pin repair, collection, and independent cleanup tests pass.

The [performance report](../performance/2026-09-12-performance-pass.md) records 22 final experiments (521 samples, zero operation failures) and preserves the earlier round that exposed seven concurrent-driver timeouts. Source batching, cycle selection, and the acquisition gate were measured without increasing lock or operation deadlines. Full final race checks and vet passed; ordinary and diagnostic binaries built successfully. Raw measurements, source fingerprints, and exact manifests accompany the report.
