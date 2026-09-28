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
