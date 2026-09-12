# Workflow intake and status report — September 11, 2026

Implemented `workflow.Engine.Submit`, `workflow.Engine.Status`, and `dockhand status` with human and JSON output. Request acceptance now persists queued work independently of a running driver. Driver cycles, control requests, and action-command submission remain unimplemented.

## Intake and model decisions

`workflow/request.go` validates phase-one action/destination/policy combinations, source identities, and resolved target descriptions. Targets are copied and sorted for deterministic request comparison; empty variant maps are normalized, and duplicate target configurations are rejected. Rebase and amend explicitly remain unsupported because their edit/base inputs have not been defined. Request IDs belong to the caller and must be reused when retrying the same logical submission.

Existing-revision requests omit `Source`. Intake resolves `InputRevision` from the ledger, derives its change identity, and records its source in the accepted `JobSpec`. A supplied change ID must agree. Mutating or publishing an existing change requires its current revision and an open disposition; verifying an older revision remains possible while the change is open. Publication requires a committed revision. Requests with no input revision supply an immutable source tree and omit the change ID. No change, revision, attempt, resource, or publication action is created during intake.

The transaction checks for a prior request before creating a job. Equivalent retries return the original receipt without another job or state commit. Different intent under the same ID is rejected, including collisions with existing control-request IDs. Later job progress or change closure does not invalidate a prior receipt. The first acceptance writes both the queued job and request index, using an opaque random job ID and the engine clock. Errors return no successful receipt; an uncertain ledger commit remains an error that callers reconcile using the same request ID.

Ledger validation now enforces both directions of the request/job index. Source retention additionally checks that each declared source commit contains its declared source tree. Object existence/type checks, pins, and guarded ref updates continue through the existing ledger machinery. These changes validate record relationships and source identity; they do not evaluate Portfiles or resolve target selectors.

## Status and CLI

Status reads one immutable ledger snapshot. Callers select all jobs or explicit job IDs; missing IDs and ambiguous/empty scopes are errors. Results are deterministic and include associated changes, revisions, attempts, publication actions, and pull requests. Resources moved from nested job status to a separate top-level collection, allowing cleanup to remain visible independently of job completion. An all-ledger status also includes resources whose attempt is absent.

A missing state ref produces empty status, while malformed state and read errors propagate. Snapshot-read time is distinct from recorded provider/forge observations. Missing observation times display as unknown. Status performs no bookkeeping and does not acquire a writer lock. Ledger construction still initializes the configured lockfile as previously specified.

The new `cli/status.go` owns command registration and rendering. Human output shows recorded work, evidence, publication, and resource states, escapes embedded control characters, and surfaces output errors. `--json` emits the typed status projection with Go field names and empty arrays. The existing application construction only instantiates provider adapters; status does not call them. Updated root help and the README, architecture, CLI design, and component status to distinguish the working status command from remaining action stubs.

## Provenance and validation

All implementation and validation code was newly authored for v2. No v1 code, comments, tests, or fixtures were copied, and no dependencies or packages were added. Added `internal/workflow/request.go` and `internal/cli/status.go`; extended the existing workflow, CLI root, ledger validation, and source-pin files.

`go build ./...`, `go vet ./...`, formatting, and whitespace checks passed. Temporary Go integration harnesses ran with the race detector against disposable SHA-1 and SHA-256 repositories. They exercised:

- Durable acceptance and reopening the repository; source pins and queued state without admission.
- Equivalent reordered targets, empty variant maps, copied caller data, conflicting request IDs, and retries after job progress and change closure.
- Action/destination/policy validation, missing or mismatched revisions, stale edits, committed publication inputs, invalid target paths, malformed IDs, and invalid source objects/commit-tree pairs.
- Scoped and complete status, associated revisions/evidence/PRs, independent cleanup records, deterministic record selection, and caller-owned results.
- Human and JSON CLI status, embedded control characters, unknown observation times, output failures, cancellation, status while the writer lock is held, and corrupt state remaining an error.
- Eight separate processes concurrently submitting one request and receiving the same job ID and receipt.
- Missing or inconsistent request indexes rejected during ledger encoding.

Harnesses and binaries remain outside the repository; no permanent test suite was added. Selector resolution, actual Portfile evaluation, build-environment selection, driver advancement, and control handling remain subsequent work.
