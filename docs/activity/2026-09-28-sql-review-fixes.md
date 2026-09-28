# 2026-09-28: the SQL review's fixes

Work on the [SQL review](../reviews/2026-09-28-sql-review.md), on branch `sql-review-fixes`, in the review's order of work.

## Indexes for the questions asked of history (findings 1, 2, 4, and 9)

Schema 23 adds six indexes and drops one:

| Index | Serves |
|---|---|
| `result_archive (repository_id, archive)` | `PruneArchives`: which results name each archive |
| `result_target (repository_id, target_id, recorded_at)` | `Reusable`: a target's results, newest first, without a sort |
| `revision_branch (repository_id, branch_id, created_at)` | `Revisions` |
| `execution_ref (repository_id, provider_ref)` | `ExecutionsReferred` |
| `event_kind (repository_id, kind, at)` | `CountEvents`, which it covers entirely |
| `checkpoint_branch (repository_id, branch_id, number)` | `Checkpoints` |

`session_live` is dropped, since nothing asked for the sessions that haven't ended.

The indexes are plain, not partial. SQLite uses a partial index only when the query's own conditions imply the index's `WHERE`, so a partial index is lost by a rewording of the query. The review found exactly that for `result_archive`: without `r.archive<>''` in the query, the planner walked branches, runs, executions, and results for each archive.

The six queries' texts are now named constants (`unnamedArchive`, `reusableQuery`, `revisionsQuery`, `referredQuery`, `countEventsQuery`, `checkpointsQuery`), so a test can ask SQLite how it would run the text the store runs.

Tests:
- `TestHistoryIsReadThroughIndexes` asserts each query's plan uses its index, and that reuse needs no sort. Without schema 23 it fails on the first query, with the review's scanning plan in its message.
- Schema 23 was applied to a copy of the live database (schema 22). `integrity_check` and `foreign_key_check` were clean afterwards.

## Commits reach the disk, and the planner keeps its statistics (findings 7 and 3)

**Durability (finding 7, decided: `fullfsync`).** Every connection now opens with `fullfsync(1)` and `checkpoint_fullfsync(1)` beside `synchronous(FULL)`. On macOS, a commit reaches the disk rather than the drive's cache, so a checkpoint recorded as prepared outlasts any power loss its Git change outlasts. The package comment now says so. A commit costs about 4 ms rather than 0.2 ms. The engine's tests, which commit often, went from about 100 s to 118 s.

`journal_size_limit(64 MiB)` returns the WAL file's space after a large transaction, such as a prune, is checkpointed.

**Planner statistics (finding 3).** `initialize` now runs `PRAGMA optimize=0x10002` once, after the schema is ready, outside the migration's transaction. `Open`'s schema work moved into `prepareSchema` to make room for it.

The review said to set `analysis_limit=400` as well, and that was wrong; the review now carries a correction. With a limit, `ANALYZE` keeps only `sqlite_stat1`'s averages and skips `sqlite_stat4`'s sampled values. By average, the two states in a 200-run test each hold 100 runs, so the planner kept walking every run to find the one that was queued. The sampled values show queued runs are few, and with them it uses `run_state`, bound parameters included. The vendored driver, like the SQLite CLI the review measured with, is built with `ENABLE_STAT4`.

`Runs` builds its query in `runsQuery`, so the test asks about the text `Runs` runs.

Tests:
- `TestEveryConnectionSyncsToTheDisk` holds four pooled connections at once and reads each one's settings.
- `TestOpeningKeepsThePlannersStatistics` records 200 runs, one queued, and shows the planner walks every run before a reopen and uses `run_state` after. It failed with `analysis_limit` set.

## Pruning archives in one statement (finding 1)

`PruneArchives` selected the unnamed archives, then deleted them with the same condition, so the `NOT EXISTS` ran twice. It is now one `DELETE … RETURNING`, sorted by digest in Go, since `RETURNING` gives rows in no promised order. It refuses a read-only transaction first, as `exec` does, since it no longer goes through `exec`.

Tests: `TestArchivesGoWhenNoLiveResultNamesThem` now prunes two archives at once, the later-kept one first by digest. Without the sort, it fails: `RETURNING` gave them in the order they were kept.
