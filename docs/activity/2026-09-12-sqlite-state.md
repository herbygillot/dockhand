# SQLite state implementation — 2026-09-12

## Commits made before implementation

- `ec812d2` — scoped resource locking, ledger/cycle performance improvements, tests, diagnostic tooling, and historical measurements.
- `fb1e7cb` — approved SQLite state-store design and related documentation.

The existing suite passed before migration. Implementation changes following those commits are left available for review.

## Implementation

- Added backend-independent `internal/state` contracts and `internal/state/sqlite`, using `modernc.org/sqlite` v1.58.0 and `database/sql`. The backend owns WAL configuration, connection lifecycle, foreign keys, strict tables, schema identity/versioning, and short transactions. Concurrent openers recheck schema state inside the migration transaction.
- Added repository registration keyed by canonical Git common directory. Linked worktrees share an entry; independent clones, including clones of the same remote, remain distinct. Views, writes, and relational constraints carry repository scope. Request identities and provider resource/run identities remain unique across the database.
- Added record-specific persistence for sources, changes, revisions, intake, controls, jobs, plans, attempts, submissions, evidence, and resources. Lifecycle and relationship fields use columns; nested values use per-record JSON. No whole-database document is decoded or rewritten.
- Migrated workflow intake, status, cancellation, and verification to the new contracts. A private workflow working set loads only one job's records for a transition, then writes changed records. It is not a persistence API or a database snapshot.
- Moved submission history into independent rows. Capacity waiting retains identity, confirmed closure permits a new submission, and resources retain their originating submission. Attempt submission/run fields are read projections; submission updates precede writes of existing attempts.
- Added indexed candidate queries with 64-item batches for controls, jobs, and cleanup. Claims and transitions remain atomic, with external calls outside transactions and ownership/generation checks before recording results. Unsupported executors produce durable needs-attention outcomes so they cannot starve later batches.
- Wired global `--db PATH`, defaulting to `$HOME/.dockhand/state.db`, through `app.Config.DBPath`. Removed `--lock-dir`, `-L`, and the older lockfile spelling. Writable state opens initialize lazily; status does not create missing databases, register repositories, or migrate schemas. Help, completion generation, and previews do not open state.
- Removed `internal/ledger`, `internal/lock`, and the executable Git-ledger performance harness. Historical reports and raw measurements remain unchanged; their implementation is reproducible from `ec812d2`. Existing Git ledger refs are neither imported nor deleted.

## Provenance and boundaries

The state contracts, SQLite implementation, repository/submission records, backend tests, and `tools/stateperf` probe were authored for v2. Existing v2 intake, provider recovery, claims, verdict handling, and cleanup logic were adapted to record-specific storage. Existing v2 behavioral tests were migrated; their small test-only snapshots support assertions, not production writes. No v1 source, prose comments, or tests were copied.

This implements the storage migration, not a complete command workflow. Real Tart builds, preparation, publication, dependent scheduling, evidence reuse, action-command submission, and persistent residency remain unfinished. Publication/PR tables, discovery, review controls, human-edit adoption, and branch-reassociation workflows remain deferred. State writes make no Git mutations or source pins; consuming executors must check the source they require.

## Validation and measurements

Backend checks cover rollback and cancellation, consistent snapshots during concurrent writes, read-only transactions, expired transaction handles, nested-call rejection, concurrent initialization and registration, schema protection, cross-repository references, immutable inputs, submission history, and global resource identity. Separate subprocesses test contending writers, termination during an uncommitted write, and competing drivers submitting exactly once.

Workflow regressions cover capacity, observation, cancellation, stale claims/results, closure before retry, retained/uncertain cleanup, bounded candidate batches, and independent progress past unsupported executors. App tests cover worktrees, two clones of the same repository, unrelated repositories, and read-only status. CLI tests cover default/explicit database paths and removal of old flags.

The [performance report](../performance/2026-09-12-sqlite-state.md) records the history-size and four-process measurements. The first run exposed broad cancellation/resource queries; targeted indexes and resource queries removed their dependence on completed history. The race detector found a subprocess-test output-buffer race, which was corrected before the passing run.

`go test -race ./... -timeout 120s`, `go vet ./...`, and executable builds passed from the migrated repository. CLI smoke checks confirmed the default database path, rejection of the old flags, and empty status without database creation. The final probe collected 960 samples with zero operation errors. Documentation links and whitespace were checked.
