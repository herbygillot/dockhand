# Workflow package documentation — 2026-09-11

## Changes

Added package documentation in `internal/workflow/doc.go` and Go documentation comments for the public engine API, configuration fields, request and result types, scope, and error values. The comments describe the current implementation and its caller contracts:

- `Submit` and `Control` persist idempotent intent; they do not start provider work.
- Acceptance, driver pickup, admission, completion, and cleanup are separate milestones.
- Request retries use the original caller-supplied intent, including after uncertain ledger commits.
- `Status` reads one immutable ledger snapshot, while a cycle result summarizes progress across several transactions and reads.
- Cycles perform bounded work and leave repeated invocation, waiting, and process lifetime to their callers.
- Per-job or per-resource problems, stale results, and errors that stop a pass have distinct reporting paths. Earlier committed progress survives a later error.
- Engine timing defaults, cooperative provider deadlines, claim ownership, and dependency requirements are explicit.

Documented internal helpers and added focused comments at the claim/call/result boundaries, submission closure, cancellation observation, and independent resource release. The comments explain transaction ownership, recovery obligations, retained resource identities, and the checks needed before adopting external results. Reserved execution dependencies and the current single-target verification scope are identified explicitly.

## Validation and provenance

Ran `go build ./...`, `go vet ./...`, and `go doc -all ./internal/workflow`. Compared Go token streams with the previous working tree to confirm that all existing declarations and executable code are unchanged. Checked public documentation coverage, formatting, and whitespace.

All comments were newly written from the current v2 implementation and its provider and ledger contracts. No v1 comments or tests were copied. No tests, dependencies, fields, signatures, serialized values, or workflow behavior were changed.
