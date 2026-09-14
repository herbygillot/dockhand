# Branch-selected waiting and cancellation

`wait` and `cancel` now accept `--branch <branch>` or default to the current local branch when no job ID is supplied. An explicit job ID remains available for exact execution control and cannot be combined with `--branch`. Detached HEAD requires an explicit selector.

Workflow resolves a branch through one open tracked contribution and freezes only its queued and active jobs. Waiting selects them within one read snapshot. Branch cancellation selects the same set and inserts the durable control in one SQLite write transaction. The control continues to contain exact job IDs, so work accepted after that commit never joins attachment or cancellation. Retrying the same cancellation request retains its original selection.

The state query gained a stable change-ID predicate. SQLite schema 10 adds `jobs_change(repository_id, change_id, id)`, and change-scoped lookups explicitly use it instead of scanning repository-local job history. Historical `status --branch` semantics remain unchanged.

Human output reports the branch and frozen job IDs. JSON action results expose `Branch` and `JobIDs` for a multi-job selection while preserving `JobID` for exact selection. The existing process manager still advances one fixed workflow scope; no alternative driver loop or process-to-process transport was added.

All implementation, tests, and documentation in this change were authored for Dockhand v2. No v1 comments or tests were copied.

## Validation

- `go test ./internal/state/sqlite ./internal/workflow ./internal/cli`
- `go test ./...`
- `go vet ./...`
- `make build`
- `git diff --check`
