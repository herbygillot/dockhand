-- Schema 24: an execution's environment, for its run and attempt, is the
-- whole environment, its developer tools included (model.Environment), as
-- it has been since schema 11 kept them; the uniqueness schema 1 declared
-- predates them (docs/reviews/2026-09-28-sql-review.md, finding 10).
--
-- A constraint can't be altered, so executions is rebuilt, and results
-- with it. A migration runs in a transaction with foreign keys enforced,
-- and dropping a table that results refer to would leave results without
-- theirs; a deferred check doesn't forgive it, even once a rebuilt table
-- is renamed into place. So the new results refer to the new executions
-- from the start, nothing refers to the old executions when it goes, and
-- renaming the new executions renames what the new results refer to.

CREATE TABLE executions_24 (
 repository_id TEXT NOT NULL,
 id TEXT NOT NULL,
 run_id TEXT NOT NULL,
 provider TEXT NOT NULL,
 platform_os TEXT NOT NULL,
 platform_version TEXT NOT NULL,
 platform_architecture TEXT NOT NULL,
 attempt INTEGER NOT NULL CHECK(attempt BETWEEN 1 AND 3),
 state TEXT NOT NULL CHECK(state IN ('waiting','running','finished','infrastructure-failed','canceled')),
 detail TEXT NOT NULL,
 provider_ref TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 finished_at INTEGER,
 developer_tools TEXT NOT NULL DEFAULT '',
 observed TEXT NOT NULL DEFAULT '',
 identity TEXT NOT NULL DEFAULT '',
 reused INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(repository_id, id),
 UNIQUE(repository_id, run_id, provider, platform_os, platform_version, platform_architecture, developer_tools, attempt),
 FOREIGN KEY(repository_id, run_id) REFERENCES runs(repository_id, id),
 CHECK((state IN ('finished','infrastructure-failed','canceled')) = (finished_at IS NOT NULL))
) STRICT;
-- rowid is carried over, here and for results: a record's order among
-- those made at the same time is the order it was made in.
INSERT INTO executions_24(rowid, repository_id, id, run_id, provider, platform_os, platform_version, platform_architecture, attempt, state, detail, provider_ref, created_at, finished_at, developer_tools, observed, identity, reused)
 SELECT rowid, repository_id, id, run_id, provider, platform_os, platform_version, platform_architecture, attempt, state, detail, provider_ref, created_at, finished_at, developer_tools, observed, identity, reused FROM executions;

CREATE TABLE results_24 (
 repository_id TEXT NOT NULL,
 execution_id TEXT NOT NULL,
 target_id TEXT NOT NULL,
 outcome TEXT NOT NULL CHECK(outcome IN ('passed','failed','blocked','interrupted','not-run','unevaluated')),
 phase TEXT NOT NULL,
 tests TEXT NOT NULL,
 log TEXT NOT NULL,
 inputs TEXT NOT NULL,
 recorded_at INTEGER NOT NULL,
 archive TEXT NOT NULL DEFAULT '',
 detail TEXT NOT NULL DEFAULT '',
 builders TEXT NOT NULL DEFAULT '',
 reused_from TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(repository_id, execution_id, target_id),
 FOREIGN KEY(repository_id, execution_id) REFERENCES executions_24(repository_id, id)
) STRICT;
INSERT INTO results_24(rowid, repository_id, execution_id, target_id, outcome, phase, tests, log, inputs, recorded_at, archive, detail, builders, reused_from)
 SELECT rowid, repository_id, execution_id, target_id, outcome, phase, tests, log, inputs, recorded_at, archive, detail, builders, reused_from FROM results;

DROP TABLE results;
DROP TABLE executions;
ALTER TABLE executions_24 RENAME TO executions;
ALTER TABLE results_24 RENAME TO results;

-- What went with the old tables.
CREATE TRIGGER results_final BEFORE UPDATE ON results
 WHEN OLD.outcome IN ('passed','failed','blocked')
 BEGIN SELECT RAISE(ABORT, 'a complete target result is final'); END;
CREATE INDEX result_archive ON results(repository_id, archive);
CREATE INDEX result_target ON results(repository_id, target_id, recorded_at);
CREATE INDEX execution_ref ON executions(repository_id, provider_ref);
