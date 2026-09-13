# State store design

This document describes the initial SQLite implementation, following the [architecture](architecture.md), [component structure](components.md), and [CLI design](cli-design.md). The Git ledger and lock-directory implementation have been removed. `internal/state` and `internal/state/sqlite` now serve intake, status, cancellation, and the single-target verification cycle. Global `--db` is wired; see the [implementation report](activity/2026-09-12-sqlite-state.md) for scope and validation.

## First slice

Implement a shared database, repository registration, request acceptance, status, and the existing single-target verification cycle: capacity waiting, submission reconciliation, cancellation, results, and independent resource cleanup. Keep current driver attachment and provider recovery semantics.

Use `internal/state` for backend-independent contracts and `internal/state/sqlite` for the first implementation. Preserve `record` for domain data and `workflow` for decisions. There is no Git ledger, source-pin manager, Git operation journal, notes exporter, generic lock service, or event-sourced workflow in this slice. Prepared-image Tart execution is now implemented; verification command wiring and current-process residency are implemented; explicit version- and revision-bump preparation are implemented; bounded automatic GitHub discovery is implemented; standalone publication is now implemented.

## Packages and contracts

```text
internal/
  record/                  Domain records and identities
  state/
    store.go               Store, Reader, Writer, Tx contracts
    query.go               Bounded queries and cursor types
    provider.go            Pool-scoped provider transactions
    errors.go              Persistence errors
    sqlite/
      store.go             Opening, connections, closing
      transaction.go       Snapshot and write transactions
      jobs.go              Intake and job persistence
      attempts.go          Attempts, submissions, evidence
      resources.go         Ownership and cleanup persistence
      provider.go          Shared pools and provider executions
      migrations/          Ordered schema changes
  workflow/                Intake and progression rules
  app/                     Database selection and dependency wiring
```

Do not create an interface per table or a generic key/value API. Introduce methods for the records and queries actually consumed by the workflow. SQL, the selected Go SQLite driver, connection configuration, migrations, and SQLite error codes stay in `state/sqlite`. `state` imports neither Git nor the concrete backend. `app` owns the opened implementation's lifetime and injects a `state.Store` into the workflow.

The interface shape is illustrated below; it is not a complete declaration of every method needed by the slice. `record.Repository`, `record.RepositoryID`, `record.AcceptedRequest`, and `record.Submission` are shared domain records. The concrete interface is defined in `internal/state/store.go`.

```go
type Store interface {
    FindRepository(context.Context, string) (record.Repository, error)
    RegisterRepository(context.Context, string) (record.Repository, error)
    View(context.Context, record.RepositoryID, func(context.Context, Reader) error) error
    Update(context.Context, record.RepositoryID, func(context.Context, Tx) error) error
}

type Reader interface {
    Job(context.Context, record.JobID) (record.Job, error)
    Attempt(context.Context, record.AttemptID) (record.Attempt, error)
    AttemptsForJob(context.Context, record.JobID) ([]record.Attempt, error)
    ReadyJobs(context.Context, time.Time, int) ([]record.Job, error)
}

type Writer interface {
    PutJob(context.Context, record.Job) error
    PutAttempt(context.Context, record.Attempt) error
}

type Tx interface {
    Reader
    Writer
}
```

`View` exposes one consistent, read-only snapshot. It does not copy the database into memory. `Update` exposes a fresh view with read-your-writes and serializable decisions; its changes commit together or roll back together. Callbacks run once, synchronously, and use the supplied context. They perform no provider, forge, Git, or other external work. A view or transaction must not escape its callback. Nested store calls inside a callback are unsupported; use the supplied reader or transaction. Cancellation is cooperative, and the backend checks the deadline before committing. Never automatically replay a callback after a conflict or uncertain commit.

Errors distinguish absent records, invalid records, conflicts, unavailable storage, unsupported schema, and uncertain commit outcomes. A receipt is returned only after confirmed commit. An interrupted caller retries using its original request identity. Repository registration is its own small atomic, idempotent operation, with a uniqueness constraint on canonical common directory. Read-only lookup never registers a repository.

## One database, multiple repositories

`--db PATH` selects the database independently of the invocation's checkout. Its default is `$HOME/.dockhand/state.db`. `app` uses Git to discover and canonicalize the selected checkout's common directory, looks up or registers it as appropriate, and passes the resulting repository ID to workflow operations. The state implementation receives ordinary paths and IDs; it does not discover Git repositories itself.

Linked worktrees share a repository entry. Separate clones have distinct entries even when their remotes match. Worktree paths belong to the invocation; they are not repository identity. A moved common directory requires explicit reassociation in a later workflow, rather than matching it automatically by remote URL.

A reader/transaction is bound to exactly one registered repository. Its lookups, joins, and mutations are scoped to that repository; an ID belonging to another repository is not accessible through that view. IDs are generated globally uniquely, and relationships use repository-qualified foreign keys so a job cannot accidentally acquire another repository's revision or attempt. Reusing a request ID for another repository conflicts with the original receipt.

For this slice, a change has one local repository and one branch association, stored directly on the change row. Its stable ID is independent of that association. A missing or renamed branch is an actionable error; no automatic branch guessing or evidence reassignment occurs. Branch uniqueness applies to active changes within a repository. Tracking the same changeline concurrently in multiple clones, cross-repository moves, and automatic identity-note discovery are deferred. They do not require changing job IDs or the meaning of already accepted inputs.

`status`, selectors, and `start` initially operate on the selected repository. An all-jobs scope means all jobs in that repository, not all repositories in the file. Several resident drivers can work on different repositories using the same database. Cross-repository listing and resident scheduling can be added through explicit scopes later; an omitted repository must never silently mean the whole database. The driver retains the repository context of each accepted job.

## Minimal data model

The following summarizes the [initial schema](../internal/state/sqlite/migrations/001.sql) and its ordered migrations, currently through schema 7. Domain IDs are text, timestamps are UTC integer milliseconds, missing values are NULL, and state values have explicit constraints. Each repository-owned table carries `repository_id`; composite foreign keys preserve that scope. Sources, revisions, accepted inputs, and submission identities are immutable through the write API. Lifecycle fields are updated explicitly.

| Table | Main data | Why it is needed now |
| --- | --- | --- |
| `repositories` | ID, unique canonical common directory, creation time | Multiple checkouts in one database |
| `changes` | ID, repository, branch ref, current revision, disposition, creation time | Existing contribution identity and revision selection |
| `sources` | ID, repository, commit/tree/base IDs, canonical identity fingerprint | One source description reused by related records |
| `revisions` | ID, change, source, preceding revision, creation time | Immutable input revisions and their relationships |
| `requests` | ID, repository, kind, canonical submitted input, digest, acceptance time, control completion time | Shared intake identity for jobs and cancellation |
| `jobs` | ID, request, source/input/result revision references, change, action/destination/policy, state, effective configuration, requested targets, resolved release, preparation claim/candidate, reused attempt and explanation, next action time, lifecycle times | Durable accepted work |
| `publications` | Job, revision, evidence, publication intent, lifecycle and push/write checkpoints | Recoverable publication and remote branch coordination |
| `pull_requests` | Change, forge/repository/number identity, latest observation | Match published revisions with pull requests |
| `control_jobs` | Request, job, applied time | Per-job cancellation progress |
| `plans` | Job, optional revision, frozen single-target plan | Preserve requested verification coverage |
| `attempts` | ID, job, target identity, immutable build choices and inputs, state, next action time, cancellation state, claim fields, last error | Current verification execution and scheduling |
| `submissions` | Submission ID, attempt, sequence, provider, run ID, state, admission/closure times | Recoverable provider identities, including closed submissions |
| `attempt_evidence` | Attempt, latest accepted verdict/observation time, diagnostic evidence and artifact/log references | Keep evidence separate from frequent claim updates |
| `resources` | ID, submission, provider handle, state, retention/release times, next action time, claim fields, last error | Ownership and cleanup after job completion |
| `provider_pools` | ID, unique resource scope, artifact directory, capacity | One Tart home shared across repository workflows |
| `image_digests` | Provider, canonical image path, file stamp, content digest | Disposable cache shared across repositories |
| `provider_executions` | Submission ID, pool, repository, attempt, resource identity, immutable provider request, lifecycle, occupancy, terminal result | Atomic admission, durable closure, and recovery before workflow adoption |

Use ordinary columns for keys, relationships, lifecycle states, scheduling, claims, and fields used by current queries. Small nested targets, variants, build options, plan details, and evidence can use JSON checked on write. They belong to individual records; there is no whole-state document. Do not store a second authoritative copy of a source or relational key inside JSON. The backend reconstructs existing domain values from the authoritative columns and referenced records.

The first plan contains one target, so normalized target graphs, separate artifact catalogues, per-port query tables, and event history are unnecessary now. Add those when dependent scheduling, artifact reuse, selector resolution, or a history consumer requires them. Preserve the existing negative and running evidence semantics; terminal results cannot be overwritten by a later poll, and retrying a build creates another attempt.

`requests.kind` distinguishes job intake from cancellation. Cancellation's domain record is assembled from the request and `control_jobs`; a separate `controls` table would add no useful lifetime here. Matching request kinds and immutable payloads are checked within intake's transaction. Job creation and its receipt are atomic. Cancellation completion means every selected job received the intent or was already terminal; it does not mean the provider stopped.

Give provider submissions their own rows instead of an array of closed IDs on an attempt. `record.Submission` retains those identities, and resources name their originating submission. `Attempt.SubmissionID` and `Run` are read projections of the latest submission; update that submission before writing an existing attempt. Sequence identifies the current/latest submission, and at most one submission per attempt can remain unclosed. A capacity refusal keeps that identity. Only confirmed provider closure permits a replacement identity, created in the same transaction as recording closure. Resources remain associated with the submission that produced them, including partial provisioning discovered during reconciliation. Scope provider run/resource uniqueness to the stable provider namespace across the whole database, not merely one repository.

Preserve foreign keys and useful uniqueness constraints: current/preceding revisions belong to their change; one job uses one request; control membership is unique; evidence belongs to one attempt; submissions have unique attempt/sequence and provider/run identities; resource handles are unique within their provider. Insert or adopt related records in one transaction. Reads of one job fetch only its related rows. SQL constraints supplement workflow validation; they do not implement publication or eligibility policy.

## Queries and coordination

Start with bounded queries for queued/due jobs, attempts by job, due attempts, unapplied controls, resources by owner, and due cleanup. Candidate queries exclude terminal work and use repository/state/time indexes. Selected-job queries use IDs directly. Index relationship keys and request identity. A status report may enumerate its selected repository; an individual write never requires that enumeration.

Preparation jobs, attempts, and resources retain `claim_owner`, `claim_generation`, and `claim_until`. Claim owner and deadline are either both present or both absent. Generation remains after release. Eligibility, ownership, submission intent, and the relevant state transition are checked and written in one `state.Tx`. Recording an external result rechecks owner, generation, lease, expected state, and relevant job intent in a fresh transaction.

Keep `next_action_at` nullable: NULL means no scheduled action. A live claim schedules reconsideration at lease expiry; a result schedules the next observation, retry, or cleanup deadline. Dependencies and cancellation update affected scheduling rows transactionally. An idle cycle reads indexed candidates, opens no write transaction, and calls no provider. A pass selects at most 64 controls, jobs, and cleanup actions each; unsupported executors produce a recorded needs-attention outcome so they cannot occupy every future batch. Pending-cleanup reporting can still enumerate outstanding obligations. A candidate can lose eligibility before claim; that is an ordinary no-op. Use deterministic ordering and bounded batches, with later cycles reconsidering work.

Data locking belongs to the backend. Workflow claims belong to its transaction boundary. Do not inject an independent lock backend to authorize a state write: checking ownership in one system and writing in another would introduce a gap. An external resource lock can have a separate interface if an executor needs it, but no `lock`/`flock` replacement or configurable lock directory is part of this slice.

Lease expiry does not prove an external action ended or that provider capacity is free. Provider submission/reconciliation retains its current idempotency and closure contract. The Tart provider now reserves capacity through `ProviderStore` transactions scoped to one canonical Tart home. The occupied set includes reservations whose external effects have not yet been reconciled. Reservations have no lease expiry; only confirmed VM shutdown frees capacity. Pool capacity and artifact directory must agree across cooperating processes. Different DB files do not coordinate claims for shared external resources. Initially require cooperating drivers using those resources to use the same DB; Git/provider preconditions still apply to external tools and uncertain operations.

## SQLite implementation

Use a Go SQLite driver behind the contract, WAL on a local filesystem, foreign keys enabled on every connection, and STRICT tables. Begin short write transactions with `BEGIN IMMEDIATE`; keep snapshot reads short. Use full commit durability for accepted intent, a bounded busy wait, and context deadlines. Retain five-second contention and thirty-second operation limits as initial configurable backend defaults, subject to validation. Backend opening/closing owns connections; workflow never handles a SQL transaction.

Use ordered migrations and SQLite's `user_version` for the installed schema number. Recognize a Dockhand database before applying migrations; reject an unrelated database rather than adding tables to it. Recheck the version under the migration write transaction so concurrent openers cannot apply the same migration twice. Report unsupported newer schemas and preserve failed migrations atomically. There is no application migration-history table initially.

The CLI resolves the default home directory and relative path. `sqlite.Open` normalizes the file location so aliases do not accidentally split coordination, creates a missing parent directory for a writable open, and preserves an existing database. Create private directories/files with restrictive permissions. CLI `--db` accepts a file path, not SQLite URI parameters or in-memory database names. The backend's read-only opening mode does not create the database, register a repository, or migrate the schema.

Help, completion generation, and previews do not open state. `status` reports empty results if the database or repository registration is absent, without creating either. Corrupt, unreadable, and unsupported databases are errors. A command that accepts or advances work opens writable state and initializes it lazily. Existing-schema read-only access may use SQLite's normal WAL machinery; it must not mutate workflow records.

SQLite references: [isolation](https://www.sqlite.org/isolation.html), [transactions](https://www.sqlite.org/lang_transaction.html), [WAL](https://www.sqlite.org/wal.html), [foreign keys](https://www.sqlite.org/foreignkeys.html), [STRICT tables](https://www.sqlite.org/stricttables.html), and [schema version pragma](https://www.sqlite.org/pragma.html#pragma_user_version).

## Git and later features

State persistence makes no Git mutations and creates no source pins. Before consuming source, an executor checks the objects it needs. Missing source preserves identity and historical evidence but can leave the affected job needing attention. A permission/read failure is not confirmed absence. Existing provider runs and cleanup continue where they do not need the missing local source. Never replace a job's source with the current branch head to make it runnable.

Explicit branch verification reads an isolated committed snapshot. Existing contributions adopt a revision during request acceptance; standalone verification creates no change/revision. The open-change lookup uses the existing repository/branch index. Revision preconditions, change updates, request receipts, and jobs share one transaction; source binding performs no Git or Tcl work inside it. This adds no tables or schema migration.

Revision preparation uses disposable materializations and retries immutable work from recorded input. The job checkpoints its candidate commit/tree/base and destination before integration, with a monotonic integration-started marker. A changed branch after interruption is inspected; ambiguity requires attention. There is no `git_operations` table or promise of atomicity between SQLite and Git.

Keep PRs and publication actions separate when publication is implemented. PR identity persists across repeated publication actions and revisions; intended publication and confirmed forge state remain distinct. Their tables, scheduling, and queries arrive with that executor. Discovery observations, review decisions, branch reassociation commands, and an optional identity-only notes namespace remain later work. No automatic notes configuration is required to open or use the database. Matching port name, version, and revision alone does not establish equivalent verification inputs.

## Migration and validation

The migration has moved intake, status, cancellation, and the existing cycle to repository-scoped state queries. `--db` passes through `app.Config.DBPath`; the old lock flags and Git ledger/lock packages are removed. Historical reports and raw results remain available. The former executable harness can be reproduced from commit `ec812d2`; `tools/stateperf` measures the SQLite implementation.

No automatic import of the experimental Git ledger or deletion of its refs is required for this prerelease transition. Leave existing repositories and their old refs untouched unless an explicit cleanup/import is requested. New state is initialized in the selected database. The implementation uses `modernc.org/sqlite` v1.58.0 through `database/sql`, with no CGO requirement or public driver dependency in the state contract.

Validation should cover two processes claiming the same work, atomic claim/state rollback, stale results, uncertain submission reconciliation, cancellation, and cleanup independent of job completion. Add two unrelated repositories and two clones of the same remote to one database; prove same-named branches and scoped queries cannot collide, cross-repository relationships are rejected, and linked worktrees share registration. Exercise concurrent registration and schema initialization, context cancellation, read-only status on missing state, and flag/help behavior.

The [SQLite performance report](performance/2026-09-12-sqlite-state.md) records representative history-size and multi-driver measurements. A claim or result write must access only its affected records and indexes, without whole-database decoding or source scans. A backend contract test suite should exercise real transactions; an in-memory fake alone cannot establish cross-process behavior. No source code or tests from v1 need to be copied for this migration.

## Tart provider persistence

Schema 2 adds `provider_pools` and `provider_executions`; schema-1 databases upgrade transactionally on writable open. Read-only opening requires the current schema. The pool interface is separate from repository-scoped workflow queries because admission must include every repository sharing the host resource pool. It uses the same backend and transaction rules, with no Tart or Git imports in `state`.

Provider execution records describe effects that may exist before the workflow adopts a run. Their immutable payload freezes the submitted build and effective provider configuration for idempotency and recovery; it is not used to redefine the accepted workflow inputs. Request IDs are unique across pools. Reserved/admitted executions reference an attempt in the same repository. An unknown ID can be permanently closed without an attempt or VM. Terminal results are immutable, and closed/released identities cannot be revived. The occupied query uses a partial index, so admission reads current reservations rather than historical executions.

SQLite capacity decisions do not fence a delayed external command. The Tart adapter also holds an OS lock per submission under the pool's artifact directory. Short Tart/launchctl subprocesses inherit its file descriptor, preserving exclusion if the driver dies while a command continues. Lock files are not unlinked, and no global lock flag or general filesystem-lock package is reintroduced. No database transaction spans cloning, booting, source transfer, guest work, stopping, or deletion.

## Recorded CLI execution choices

`BuildConfig.ProviderConfig` is an optional bounded JSON object containing the provider-specific execution choices captured before acceptance. Tart uses it to recover image, platform, guest prefix, executable, home, and artifact settings for queued work even in a fresh process. Existing admitted executions retain their original payload. Capacity omission uses the immutable registered pool limit. Intake copies the mutable JSON value and planning preserves it; settings are included in accepted request identity and cannot be silently replaced during retry.

This field fits the existing per-job/per-attempt configuration columns, so this slice adds no table or schema version. `ProviderStore.ProviderPool` provides a read-only pool lookup for consistent defaults. Driver residency is a process lifetime, not a new durable entity; workflow state and claims already provide coordination. Credentials are not part of this configuration.

## Preparation checkpoints and standalone verification

Schema 3 adds job claim owner/generation/expiry, retry time, and a small `prepared` JSON checkpoint. Job options also carry accepted preparation source-branch, author, platform, and any verification setup problem. The checkpoint's branch and source are immutable once present; integration-started cannot be cleared. Confirmed integration creates the change/revision and sets the job's result revision in one transaction. The candidate source is intentionally retained on the job for recovery and verified-result selection; it is copied into the immutable revision when adopted.

Plans and attempts allow a NULL revision reference for standalone verification. Their job and source references remain mandatory. The storage API checks revision selection against the owning job and checks standalone attempt source identity; removing a contribution requirement does not remove repository or build-input identity. Existing tracked jobs, attempts, submissions, evidence, resources, and provider executions survive migration. Foreign-key checks run before completing the transactional table rebuild.

Job scheduling includes preparation claims and retries. Cancellation before any candidate exists can settle immediately because preparation only writes immutable objects; a later preparation result still has to prove ownership. Integration cancellation respects branch exclusion and uncertain effects. Git's branch lock is operation-specific and remains outside the state contract. Read-only opening does not migrate older databases; a writable command performs the upgrade.


## Resolved release checkpoint

Schema 4 adds nullable `jobs.resolved_release` JSON. It records one bump's requested spelling (empty for automatic selection), effective version, GitHub repository, exact tag, peeled commit, and observation time. The write API requires a matching bump request and valid commit identity, then forbids replacement or removal once present. This is a job result checkpoint, separate from immutable accepted intent and from the later prepared candidate.

The driver records the release in one claimed pass and prepares the source in a later pass. No database transaction spans tag lookup, source evaluation, or downloading. Failed checkpoint writes cannot start preparation; expired or canceled lookup claims cannot overwrite a later result. Scheduling permits cancellation before preparation for both bump actions. Existing schema-3 candidates and integration intent remain unchanged on migration; writable opening upgrades older schemas transactionally, while read-only opening requires the current schema. Archive bytes, HTTP credentials, and full preparation diagnostics are not stored in this column.

Automatic selection adds optional `CurrentVersion` and `NoUpdate` fields inside the same JSON column; schema 4 and existing explicit-release rows remain valid. The store requires an input version for automatic selections. A `NoUpdate` checkpoint is valid only for an automatic bump completed without a prepared candidate or result revision. The driver writes the release and completion atomically, so checkpoint failure cannot appear as successful completion. No branch or attempt is created for this outcome; verification was unnecessary for the requested no-op, not recorded as passed.

## Checkout provenance

Working-tree verification uses the existing nullable source commit and mandatory source tree. Optional `JobSpec.Checkout` records the selected checkout's branch (empty for detached HEAD), observed HEAD commit, and modified-file count in `jobs.options` JSON. This is immutable accepted-input provenance, distinct from `Source.Base` and from a claim that HEAD contains the tested edits. Clean captures retain the matching source commit; dirty captures leave it empty. No source or job table migration is needed. Reopening the database and repeated driver cycles preserve both provenance and exact source identity.


## Verification reuse

Schema 5 adds nullable `jobs.reused_attempt`, which references the original `attempts` row, and `jobs.reuse_detail`. The write API checks repository scope, a terminal passing attempt for the same tree, completed job state, and absence of this job's own executions. A recorded reference cannot be replaced or removed. Full input comparison belongs to `verify` and is applied by workflow in the same transaction as the reference, plan, and completion. No new evidence or provider execution is synthesized.

`VerificationCandidates` returns at most 32 original terminal attempts with evidence, ordered by attempt creation time descending and ID descending for ties. Tree and target indexes narrow the search within the selected repository; an optional tree-less lookup supplies one recent result for mismatch diagnostics. The limit bounds records materialized by the reader, not the number of matching index entries SQLite might visit. Negative outcomes are included so an older pass cannot hide a newer failed recheck. Reused jobs never become candidates themselves.

`FreshVerification` fits immutable job options. `BuildConfig.VerifierDigest` fits existing build JSON; old records with an absent digest remain readable and executable but cannot supply reusable evidence. Tart binds the current digest at intake and refuses new submission if a recorded nonempty digest differs from the running verifier. Writable opening upgrades older schemas atomically; read-only opening requires the current schema. Migration failure rolls back the new columns and indexes together.

## Publication storage (schema 6)

`publications` stores one immutable intent per job, tied by repository-qualified foreign keys to its job, contribution revision, and evidence attempt. Separate columns hold lifecycle state, irreversible push/write checkpoints, confirmation time, and diagnostics. A partial unique index on forge/head-repository/head-branch reserves active or uncertain work across all repository entries in the database. Confirmed and definitively rejected actions retain history without keeping that reservation. Recovery keeps using the job claim and eligibility columns.

`pull_requests` retains the latest observation and stable forge/repository/number identity for each change. `changes.published_revision` and `changes.pull_request_id` retain publication provenance and association; writers validate both within the scoped repository/change. Jobs retain their accepted publication choices in their options, and action writers enforce agreement and immutable intent. Small publication/PR JSON values are per record, not full-state snapshots. The adapter does not track unrelated Git commands.

Migration 6 preserves existing source, revision, job, verification, and provider history. The reader adds point lookups by publication job and PR ID, used by consistent status snapshots. No broad action enumeration or second coordination interface is required by this slice.


## Image digest cache (schema 7)

`image_digests` has a composite primary key on provider and canonical image path. Each row stores one complete stamp/digest pair, replaced by an upsert. It has no repository foreign key: repositories using the same image and database share the observation. Cache loss merely requires rehashing and cannot erase accepted input digests or verification evidence.

`state.ImageCache` supplies point lookup and replacement, implemented by the same SQLite store as provider coordination. The provider owns stamp interpretation, content hashing, and invalidation. Reads and writes each use a short transaction; hashing does not hold a transaction or reserve a provider slot. Concurrent cold readers can hash independently. A delayed writer can replace a newer row, but its old stamp will not match changed files on a later lookup. No cache row claims that a VM is available, stopped, or safe to delete.

Migration 7 adds only the cache table and preserves workflow/publication/provider records. It participates in the existing transactional migration chain; a failed migration leaves the previous schema intact.
