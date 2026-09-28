# SQL review

Reviewed 2026-09-28 at `02a4d31867576c76a6fd2b668b3ed4ac5a98ce57` (`refactor(github): GitHub's addresses are read and written in the GitHub layer`). The subject is every statement dockhand sends to its database: the 22 migrations in `internal/store/sqlite/schema/`, the queries in `internal/store/sqlite/` (`records.go`, `history.go`, `coordination.go`, `sqlite.go`), and the engine code that calls them. Nothing else in the tree issues SQL. The questions were whether each query reads only what its caller needs, which indexes or other structures the questions dockhand asks often call for, and what should change in the schema or database settings.

## Assessment

The SQL has not rotted. The package is small (about 1,100 lines of Go), every table is `STRICT` and keyed by `(repository_id, …)`, and nearly every read is a primary-key or index lookup. Several rules the schema can check, it does: state CHECKs, final-verdict and lease-generation triggers, unique run and snapshot numbers.

What churn left behind is a handful of queries whose cost grows with **total history** rather than with the question asked. Each was fine when written and its table was small. None of `runs`, `executions`, `results`, `inputs`, `plans`, or `revisions` is ever pruned, so these scans only grow. One of them, archive pruning, becomes unusable at about a year of serve use, while holding the write lock.

The second theme is smaller: validation reads that fetch and decode whole records to learn one fact. SQLite runs in-process, so these cost microseconds to a millisecond, not network round trips. They are worth fixing where they sit inside write transactions, not otherwise.

P1 below means a query that fails at plausible scale. P2 means a measured cost that grows with history, or a guarantee the code states but does not provide. P3 means a cleanup or a latent issue. Citations are repository-relative `path:line` at the reviewed commit.

## Method

The live `~/.dockhand/dockhand.db` is v3's, started 2026-09-25. It holds 1 repository, 9 branches, 11 runs, 11 results and 247 events, too few to show any plan's cost. It was copied to a scratch directory and used only for per-row sizes: plan bodies are about 640 bytes for small checks, and `inputs` records average 3.6 KB (303 B to 7.4 KB).

Scale was modeled with a synthetic database built from the real migrations, sized at roughly a year of heavy serve use: 300 branches, 6,000 revisions, plans and runs, 18,000 executions (3 per run), 200,000 results drawn from 2,000 port names, 20,000 kept archives, 30,000 events (30 days' retention), and 60 sessions. Each production query was run as written under `EXPLAIN QUERY PLAN` and timed with the SQLite CLI. The vendored `modernc.org/sqlite` bundles SQLite 3.53.4, the same version as the CLI, so the plans are the ones dockhand gets. The appendix builds the database and has the queries.

Go-side costs (driver pragmas, commit latency, plan decoding) were measured through the tree's own `Open` and `decodePlan` with the [companion probe patch](2026-09-28-sql-probes.patch). Timings are from an Apple M5 Max.

## Findings

### 1. [P1] `PruneArchives` reads every result once for each archive, twice, under the write lock

**Evidence:** `internal/store/sqlite/records.go:664`; called from `internal/engine/archives.go:99` in daily cleanup (`internal/engine/clean.go:630`), inside `Store.Update` and so inside `BEGIN IMMEDIATE`.

The `NOT EXISTS` subquery joins `results → executions → runs → branches` on `r.archive = a.digest`. `results` has no index on `archive`: its primary key begins `(repository_id, execution_id, …)`. For every candidate archive, SQLite therefore walks every result in the repository:

```
SEARCH a USING INDEX sqlite_autoindex_archives_1 (repository_id=?)
CORRELATED SCALAR SUBQUERY 1
   SEARCH r USING INDEX sqlite_autoindex_results_1 (repository_id=?)
```

The same condition is then evaluated again by the `DELETE` that follows the `SELECT`.

**Reproduced:** with 20,000 archives (about 15,000 older than the cutoff) and 200,000 results, the `SELECT` alone ran past 120 seconds and was killed: about 3 billion row visits. At the default 30-second `OperationTimeout`, cleanup would time out and roll back, leaving the archives on disk forever, and every writer (serve, checks, heartbeats) would wait on the lock until it did.

With `CREATE INDEX result_archive ON results(repository_id, archive)`, the unchanged query takes **27 ms**:

```
SEARCH r USING INDEX result_archive (repository_id=? AND archive=?)
SEARCH e USING INDEX sqlite_autoindex_executions_1 (repository_id=? AND id=?)
…
```

A partial index (`WHERE archive<>''`) is smaller, but SQLite uses it only if the query also says `r.archive<>''`: `r.archive = a.digest` does not imply it. Without that term, and with statistics present, the planner chose `SCAN b` and walked branches, runs, executions and results per archive, just as badly. Because a partial index here depends on the query's wording, and a later edit could quietly drop that term, the plain index is the recommended one.

**Remedy:** add the index. Also make the operation one pass: `DELETE FROM archives AS a WHERE … RETURNING digest, name, size, kept_at`, sorted in Go since `RETURNING` has no order. SQLite has supported `RETURNING` since 3.35.

### 2. [P2] `Reusable` scans every result in the repository, per target, per environment

**Evidence:** `internal/store/sqlite/records.go:538`; called for each remaining target of each environment before its first attempt (`internal/engine/reuse.go:29`).

The query filters `results` on `target_id`, `outcome`, `inputs` and `reused_from`, none of which leads an index, and sorts by `recorded_at` in a temporary B-tree:

```
SEARCH r USING INDEX sqlite_autoindex_results_1 (repository_id=?)
SEARCH e USING INDEX sqlite_autoindex_executions_1 (repository_id=? AND id=?)
USE TEMP B-TREE FOR ORDER BY
```

**Reproduced:** 9.5 ms per call at 200,000 results. A 300-target check in 6 environments makes 1,800 calls, about 17 seconds, and the cost grows linearly with results kept.

With `CREATE INDEX result_target ON results(repository_id, target_id, recorded_at)`, the unchanged query takes about **0.1 ms** and needs no sort. The index includes the rowid, so `ORDER BY recorded_at DESC, rowid DESC` is a backward walk that stops at `LIMIT`:

```
SEARCH r USING INDEX result_target (repository_id=? AND target_id=?)
SEARCH e USING INDEX sqlite_autoindex_executions_1 (repository_id=? AND id=?)
```

A partial index on `outcome='passed' AND inputs<>'' AND reused_from=''` also works, because the query states those terms literally. It is smaller, but it is tied to the query's wording as in finding 1, and the plain index also serves any future per-target question.

### 3. [P2] The database has no planner statistics, so `Runs(states)` reads every run

**Evidence:** `internal/store/sqlite/records.go:318`. `Runs` with states and no branch is called by serve's `Candidates` on every 2-second poll (`internal/engine/runner.go:1004`, `internal/engine/serve.go:261`), and by `queue` and `status` (`internal/command/queue.go:34`, `internal/command/status.go:367`). Nothing runs `ANALYZE` or `PRAGMA optimize`; `analysis_limit` is 0.

`run_state (repository_id, state, number)` exists for this query, but with no statistics the planner prefers walking the `UNIQUE(repository_id, number)` index to avoid a sort. It reads every run in number order to find the few queued ones:

```
SEARCH runs USING INDEX sqlite_autoindex_runs_2 (repository_id=?)
```

**Reproduced:** 6.6 ms at 6,000 runs, 9 of them active. After one `PRAGMA optimize=0x10002`, it uses `run_state` and takes **0.08 ms**. The first `optimize` on the 200,000-result copy took 136 ms. The statistics are stored in the database (`sqlite_stat1`), so one process's analysis serves the next.

**Remedy:** in `initialize`, after migrating, run `PRAGMA analysis_limit=400; PRAGMA optimize=0x10002;` once per process. This is SQLite's documented recommendation for applications. `analysis_limit` bounds the cost on large tables. The per-connection DSN pragmas are the wrong place, since `optimize` would run on every new pooled connection.

### 4. [P3] Four more lookups scan the repository's rows for want of an index

Each is small today, and each grows with history.

- **`Revisions(branch)`** (`internal/store/sqlite/records.go:151`). The only `branch_id` index on `revisions` is the partial `revision_snapshot`, which this query cannot use. It is called by `treeRuns` for each branch that `status` reports on, and so for each open branch every time serve refreshes its submit candidates, and also by capture and clean. Index: `revisions(repository_id, branch_id, created_at)`.
- **`ExecutionsReferred(ref)`** (`records.go:438`) scans `executions` for a `provider_ref`. It is called for each leftover `clean` judges and by `logs <URL>`. Index: `executions(repository_id, provider_ref)`. Use a plain index for the reason given in finding 1: a partial `WHERE provider_ref<>''` is used only if the query repeats the predicate.
- **`CountEvents(kind, since)`** (`internal/store/sqlite/coordination.go:161`) scans the retained journal. It is called by serve's submitter for each candidate at each refresh (`internal/engine/serve.go:544`). About 27 ms before, and 0.015 ms with `events(repository_id, kind, at)`, which covers the query entirely.
- **`Checkpoints(branch)`** (`internal/store/sqlite/history.go:113`) scans `checkpoints`. The table grows by one row per tidy or rebase, so this is the least urgent. Index: `checkpoints(repository_id, branch_id, number)`.

Building five indexes on the synthetic copy took 1 to 130 ms each and made the file about a third larger (37.8 MB to 49.6 MB). Results are written once per target build, so the extra cost per insert is negligible.

### 5. [P3] `RecordResult` decodes whole records to learn that they exist

**Evidence:** `internal/store/sqlite/records.go:572`–`:600`.

For each result recorded, inside the write transaction, `RecordResult`:

- calls `t.Inputs(key)`, which reads and JSON-decodes the whole inputs record (3.6 KB on average, up to 7.4 KB live), only to learn that it exists;
- calls `t.execution(r.ReusedFrom)`, which reads all 16 columns and decodes `observed`, again only for existence;
- decodes the run's entire plan to check that the target is in it.

The first two should be `SELECT 1 … LIMIT 1`, or a small `exists(table, key)` helper.

**Measured:** plan decoding costs 3.7 µs for a 2-target plan, 92 µs for 50 targets × 3 environments (32 KB), and 0.85 ms for 300 targets × 6 environments (309 KB). For the large plan that is about 1.5 seconds of write-lock time over a run's 1,800 results, which is real but modest. Plans are immutable once added ("A plan is frozen at creation", `schema/001.sql`), so a small cache of decoded plans inside the store, used only for its own validation, is safe. `AddExecution` and `AddRun` would use it too. Keep the cache off `Plan()`'s return path, because callers receive slices and maps they could change.

### 6. [P3] Reuse re-reads the executions its own query already joined

**Evidence:** `internal/engine/reuse.go:29`–`:40`; `internal/store/sqlite/records.go:540`.

`Reusable` joins `executions` to filter by environment, then discards the columns it joined. `earlier` then calls `r.Execution(result.Execution)` and `r.Inputs(result.Inputs)` for every candidate, up to 5 per target. Many candidates share an execution, because one earlier check built many of these targets, and the same inputs key can recur. Either `Reusable` returns each result's origin execution from the join it already makes, or `earlier` caches executions and inputs by key for the duration of its `View`. The first keeps the join's result with the store, which owns it.

### 7. [P2] "Full sync" is not full on macOS

**Evidence:** `internal/store/sqlite/sqlite.go:4` ("opened in WAL mode with foreign keys on and full sync"); the pragmas at `sqlite.go:92`–`:96`.

`synchronous(FULL)` makes SQLite call `fsync()` at each commit. On macOS, `fsync()` hands data to the drive but does not flush the drive's cache; only `fcntl(F_FULLFSYNC)` does, and SQLite uses it only when `PRAGMA fullfsync=1`. **Measured through `Open`:** `fullfsync = 0` and `checkpoint_fullfsync = 0`. After a power loss, commits the code treats as durable can be missing.

This matters most for the checkpoint protocol (schema 14, `internal/store/sqlite/history.go:93`). It records a `prepared` row, makes the Git change, then settles the row, so that a stopped process can be finished "from what Git shows". The protocol is write-ahead, but on macOS without `F_FULLFSYNC`, the Git change can survive a power loss that the prepared row does not. A process or kernel crash alone does not lose data; a loss of power does. That makes this a concern for desktop Macs running serve more than for laptops.

**Measured per commit** (one event per write transaction):

| Setting | ms per commit |
|---|---|
| `synchronous=FULL`, `fullfsync=0` (today) | 0.16–0.20 |
| `synchronous=FULL`, `fullfsync=1` | 4.1–4.4 |
| `synchronous=NORMAL` | 0.05–0.13 |

dockhand commits a few times a second at most: events, heartbeats, results. 4 ms per commit is affordable.

**Decision needed:** either turn on `fullfsync` (and `checkpoint_fullfsync`) so the comment is true, or keep today's behavior and change the comment to say what it provides. The review recommends `fullfsync`.

### 8. [P2] Build history, and `inputs` above all, is kept forever

**Evidence:** the only `DELETE`s in the store are `events` and `sessions` (`internal/store/sqlite/coordination.go:140`) and `archives` (`records.go:664`).

`runs`, `executions`, `results`, `plans`, `revisions` and `inputs` are never removed, even for merged branches. That retained history is what makes findings 1, 2 and 4 grow. By bytes, `inputs` grows fastest: one record per distinct set of build inputs, at about 3.6 KB each. Nearly every build reads a distinct tree, so the count approaches the number of builds. Extrapolated from the live average, 200,000 builds would be about 700 MB. That figure is an estimate from nine live records, not a measurement.

The records serve a purpose. Reuse reads the newest few passed results per target and environment, and evidence reads a branch's checks. But `inputs` referenced only by results that can no longer be a reuse candidate, and the runs of branches merged long ago, serve no reader dockhand has today.

**Decision needed:** a retention rule for build history, most usefully for `inputs`. It could key on the same cutoff as archives: unreferenced by any result that is still a reuse candidate, or that belongs to an open branch. The rule is a design decision, which is why this review names it and does not prescribe it.

### 9. [P3] `session_live` is never used

**Evidence:** `internal/store/sqlite/schema/001.sql:129`. No query filters on `ended_at IS NULL`. Sessions are read by primary key (`coordination.go:26`), and pruning uses `COALESCE(ended_at, heartbeat_at)` (`coordination.go:145`). Heartbeats don't touch the index's columns, so its upkeep is small, but it is dead weight. Drop it.

### 10. [P3] The executions `UNIQUE` constraint predates developer tools

**Evidence:** `internal/store/sqlite/schema/001.sql:94` declares `UNIQUE(repository_id, run_id, provider, platform_os, platform_version, platform_architecture, attempt)`. Schema 11 added `developer_tools`, and `model.Environment` (`internal/model/plan.go:120`) includes it in an environment's identity: `AddExecution` and `UpdateExecution` compare whole environments.

A plan with two environments that differ only in developer tools would fail its second execution's insert with `ErrConflict`. This is **latent**, not live. Tart today chooses one kind of developer tools per macOS release (`internal/buildenv/tart/provider.go:175`), so no plan can contain such a pair. Add `developer_tools` to the constraint when `executions` is next rebuilt, or before any provider offers both kinds for one platform. SQLite cannot alter a table constraint in place, so this means a table rebuild like schemas 4 and 8.

## What holds up

These were checked and need no change:

- **Point reads** (`Branch`, `Run`, `RunNumbered`, `Execution`, `Plan`, `Lease`, `Session`, `Checkpoint`, `Inputs`, `Archive`) are primary-key or unique-index lookups.
- **`BranchNamed`** uses the partial unique `branch_name`. `NextSnapshot` uses `revision_snapshot`. `NextRunNumber` and `NextCheckpointNumber` take `max()` from an index.
- **`Runs(branch, …)`** uses `run_branch`. `Results(execution)` and `Executions(run)` seek by key prefix. Their small sorts are harmless.
- **`Events`, `RunEvents` and `LastEvent`** use `event_repository` and `event_run`. Schema 21 fixed the one follower that scanned. `watch` filters levels in Go, but reads only new events.
- **`events` uses `AUTOINCREMENT` correctly.** Pruning can delete every row, and followers hold sequence numbers, so a sequence must never be reused.
- **The transaction wrapper's** `SELECT 1 FROM repositories` is a primary-key read. Serve's per-tick `Fenced` write transaction writes nothing, so its commit touches neither the WAL nor the disk.
- **Views would add nothing.** SQLite has no materialized views; a view is a stored query text. Prepared-statement reuse was not measured. Every statement here is a short lookup, and the complexity of caching statements across `sql.Conn`s is not justified without a measurement showing a need.
- **Foreign keys whose child columns lack indexes** cost only when a parent row is deleted. The only parents deleted are sessions, and `leases` is tiny.

A smaller setting worth taking along with finding 3: `journal_size_limit` is −1, so the WAL file stays at its largest size after a big transaction such as a prune. Setting it (for example to 64 MiB) truncates the WAL at checkpoints.

## Proposed order of work

1. `schema/023.sql`, which fixes findings 1, 2, 4 and 9 with no change to query text:
   ```sql
   CREATE INDEX result_archive ON results(repository_id, archive);
   CREATE INDEX result_target ON results(repository_id, target_id, recorded_at);
   CREATE INDEX revision_branch ON revisions(repository_id, branch_id, created_at);
   CREATE INDEX execution_ref ON executions(repository_id, provider_ref);
   CREATE INDEX event_kind ON events(repository_id, kind, at);
   CREATE INDEX checkpoint_branch ON checkpoints(repository_id, branch_id, number);
   DROP INDEX session_live;
   ```
2. `PRAGMA analysis_limit=400; PRAGMA optimize=0x10002;` once in `initialize` after migrating, plus `journal_size_limit` (finding 3).
3. `PruneArchives` as a single `DELETE … RETURNING` (finding 1).
4. Existence reads in `RecordResult`, and reuse's origin taken from `Reusable`'s join (findings 5 and 6). Add the store-internal plan cache only if the measured 0.85 ms per large-plan result is judged worth it.
5. The durability decision (finding 7) and the retention rule (finding 8), each settled before its code is written.
6. Finding 10, when `executions` is next rebuilt.

Items 1 and 2 need a test that the indexes are used as intended: an `EXPLAIN QUERY PLAN` assertion per query in `sqlite_test.go`, which will catch a later edit to a query's wording that stops it using its index.

## Appendix: reproducing the synthetic measurements

The Go probes are in [2026-09-28-sql-probes.patch](2026-09-28-sql-probes.patch). They measure and assert nothing. In a disposable checkout of the reviewed commit:

```bash
git apply docs/reviews/2026-09-28-sql-probes.patch
go test -run SQLReview -bench SQLReview -count=1 -v ./internal/store/sqlite/
```

The synthetic database is built from the real migrations with the SQLite CLI, in a scratch directory (`syn.db`, about 38 MB):

```sql
-- gen.sql: run as
--   ( for f in internal/store/sqlite/schema/*.sql; do cat $f; echo; done; cat gen.sql ) | sqlite3 syn.db
PRAGMA journal_mode=WAL; PRAGMA synchronous=OFF; PRAGMA foreign_keys=OFF;
BEGIN;
INSERT INTO repositories VALUES('repo_a','/src/ports/.git',0);
WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<299)
INSERT INTO branches(repository_id,id,name,base,worktree,managed,title,state,created_at)
SELECT 'repo_a','br_'||i,'b'||i,'base','/wt/'||i,1,'t',CASE WHEN i%3=0 THEN 'open' ELSE 'merged' END,i FROM n;
WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<5999)
INSERT INTO revisions SELECT 'repo_a','rev_'||i,'br_'||(i%300),'commit',0,'c'||i,'tree_'||(i/2),'base','h'||i,1000+i FROM n;
WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<5999)
INSERT INTO plans SELECT 'repo_a','plan_'||i,'rev_'||i,'{}',1000+i FROM n;
WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<5999)
INSERT INTO runs(repository_id,id,number,branch_id,revision_id,plan_id,origin,state,detail,created_at,finished_at)
SELECT 'repo_a','run_'||i,i+1,'br_'||(i%300),'rev_'||i,'plan_'||i,'serve',CASE WHEN i>5990 THEN 'queued' ELSE 'passed' END,'',1000+i,CASE WHEN i>5990 THEN NULL ELSE 2000+i END FROM n;
WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<17999)
INSERT INTO executions(repository_id,id,run_id,provider,platform_os,platform_version,platform_architecture,developer_tools,attempt,state,detail,provider_ref,observed,identity,created_at,finished_at)
SELECT 'repo_a','ex_'||i,'run_'||(i/3),'tart','darwin',CAST(23+(i%3) AS TEXT),'arm64','xcode',1,'finished','','tart-clone-'||i,'','id',1000+i,2000+i FROM n;
WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<199999)
INSERT OR IGNORE INTO results(repository_id,execution_id,target_id,outcome,phase,tests,log,inputs,recorded_at,detail,builders,archive,reused_from)
SELECT 'repo_a','ex_'||(i/11),'port-'||((i*7919)%2000),CASE WHEN i%10=0 THEN 'failed' ELSE 'passed' END,'','none','logs/'||i||'.log','in_'||i,1000+i,'','','sha256:'||i,CASE WHEN i%5=0 THEN 'ex_1' ELSE '' END FROM n;
WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<19999)
INSERT INTO archives SELECT 'repo_a','sha256:'||(i*10),'a'||i||'.tbz2',1000000,1000+i*10 FROM n;
WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<59)
INSERT INTO sessions SELECT 'repo_a','ses_'||i,'serve',100+i,'x','v',i,i,CASE WHEN i<59 THEN i+1 END FROM n;
WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<29999)
INSERT INTO events(repository_id,at,session_id,branch_id,run_id,target_id,kind,level,message)
SELECT 'repo_a',1000+i,'ses_1','br_'||(i%300),'run_'||(5000+i/30),'',
 CASE WHEN i%500=0 THEN 'serve.submit' WHEN i%3=0 THEN 'progress' ELSE 'target.result' END,
 CASE WHEN i%4=0 THEN 'verbose' ELSE 'info' END,'a message of about sixty bytes that describes what happened ok' FROM n;
COMMIT;
```

Each finding's query is the production text from its cited line, with literals in place of parameters: for instance `r.repository_id='repo_a' AND r.target_id='port-42'` for `Reusable`, and `a.kept_at<150000` with `r.recorded_at>=150000` for `PruneArchives`. Run under `.timer on`, before and after the indexes in "Proposed order of work" and a `PRAGMA optimize=0x10002`. Run `PruneArchives` on a copy, with a timeout, before the index exists.
