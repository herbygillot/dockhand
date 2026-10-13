-- Schema 31: the indexes history's pruning reads through (the SQL study of
-- 2026-10-13, SQL-S1 and SQL-S3). Pruning an ended branch's history deletes
-- plans, revisions, runs, and executions, so each column that names one of
-- them as a parent is indexed: a delete checks its foreign keys there, and
-- the prune's own NOT EXISTS reads them. Without them PruneHistory went
-- quadratic in history and passed its timeout at a year's scale.
-- result_archive has no reader since D6 rewrote archive pruning around the
-- newest builds, and costs a write per result.

CREATE INDEX run_plan ON runs(repository_id, plan_id);
CREATE INDEX run_revision ON runs(repository_id, revision_id);
CREATE INDEX plan_revision ON plans(repository_id, revision_id);
CREATE INDEX result_reused ON results(repository_id, reused_from);
DROP INDEX result_archive;
