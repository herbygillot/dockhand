# CLI execution and residency — 2026-09-12

## Implemented

- Committed the preceding Tart implementation as `6bdeb41` (`feat: execute recoverable Tart verification runs`).
- Wired `verify`, `wait`, `cancel`, and `start` through the existing workflow engine. Verification selects one port from a committed literal branch, with optional subport/variants, prepared image, shared capacity, test policy, and source-build choice. The default branch is the current local branch; detached HEAD needs an explicit branch.
- Added `workflow.Reached` for recorded admission/completion milestones and replaced the process-manager placeholders with a shared cancellable cycle loop. Default verification waits for admission; wait/trace follow completion. Resident execution handles the selected repository and avoids full-history status projection. Multiple residents use the existing state claims without a singleton lock.
- Added signal-context handling in main. Interruption detaches without creating a cancellation request. Explicit cancellation records intent and runs a cycle; `--wait` continues through settlement. Already completed outcomes are preserved.
- Added structured action results and distinct exit codes. Progress and trace go to stderr; JSON result documents stay on stdout. Accepted job IDs and last observed state remain available when attachment is interrupted. Status remains read-only.
- Added optional bounded log reading to the verification interface. Tart streams guest log ranges and reads preserved host logs after completion. The CLI tracks offsets and drains terminal logs. Log read errors are visible without rewriting the build verdict.
- Added optional provider configuration to accepted build settings and a read-only provider-pool lookup. Fresh processes can submit queued work and observe admitted work without repeating image/capacity flags. Explicit conflicting pool configuration is rejected. No tables, schema version, dependency, or additional package were introduced.

## Provenance

The commands, attachment loop, milestone predicate, application binding helper, provider configuration capture, log reader, signal handling, and tests were authored for v2. Existing v2 source binding, request intake, cancellation, SQLite state, provider recovery, and status rendering are reused. No v1 comments or tests were copied. The former unused process-discovery/registry placeholders were removed; persistent execution runs within the current invocation.

## Validation

Tests cover admission versus completion attachment, continued progress after per-job problems, terminal outcomes, state errors, scope rejection, interruption without another cycle, resident execution without status-history reads, and resident application of accepted cancellation controls. CLI tests cover invalid arguments before state initialization, JSON/progress separation, log offsets/final draining, failure exit codes, wait without fresh submission, cancellation before admission without Tart, branch preservation, and foreign-repository job rejection. Provider tests cover frozen choices, omitted recovery flags, recorded capacity, and readable logs after resource release.

The full suite and race detector passed. The affected CLI/process packages were checked again after the resident and output refinements. `go vet ./...`, `go build ./...`, formatting checks, and `git diff --check` passed.

A real compiled CLI was exercised against an authored no-download fixture on Darwin 25 / arm64 with Tart 2.36.0 and `dockhand-base-tahoe`:

```sh
dockhand --db <temporary-db> --json verify dockhand-fixture --image dockhand-base-tahoe --capacity 1
dockhand --db <temporary-db> --json wait <returned-job-id> --trace
dockhand --db <temporary-db> --json start
```

Verification exited successfully at admission with the job still active. A fresh `wait --trace` process, with no image or capacity options, completed the same run, captured indexing/lint/build/test/install logs, and released the VM. Both stdout outputs parsed as JSON. The successful job was `job_LFO6LM5IGGCYBGPRTTD3S7U25G`. Sending SIGINT to the real resident process returned code 130 and a stopped/interrupted JSON result, without canceling recorded work.

An earlier live attempt encountered Tart's `NIOFcntlFailedError` guest-control failure. Its readiness call timed out; Dockhand recorded a needs-attention outcome, returned exit code 3, and confirmed resource release. The subsequent independent run passed. This was not treated as a build pass or hidden behind an unlimited retry loop. No Tart upstream change was made.

## Current limits and next work

This is one-port verification bound to a committed branch. A tracked branch currently has its accepted target association; an unrelated target on the same branch is rejected. General selectors, branch-only port inference, dirty-worktree adoption, and their user-facing policies remain future work. Wait/cancel take exact job IDs. Image configuration is explicit through the flag or Go application configuration; general Git configuration loading is not implemented yet.

`start` is foreground and repository-scoped. There is no daemon discovery or automatic background driver. If no invocation runs cycles for an admitted job, its VM can keep occupying capacity until a later cycle collects the result. Interrupting an in-flight external call can leave a live claim that another process must wait to expire before reconciliation. `wait` ends at the job's outcome; independent retained or uncertain cleanup remains visible in status.

Cold image hashing and full-tree materialization/transfer retain their costs from the provider slice. Prepared-image provisioning, preparation/bump executors, dependent scheduling, publication, and review controls remain unfinished. The next substantial executor should prepare a concrete port change through the same intake/cycle path, after clarifying source selection for human edits.
