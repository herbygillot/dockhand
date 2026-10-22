-- Schema 37: a pull request's failed CI jobs, each classified once by
-- what made it fail, from its log (v0.5.0 part 4, item 1): a job's log
-- for one attempt never changes, so its reading is an observation of the
-- pull request's head, kept where status reads it, never read again. A
-- re-run or a new push is a new job, and a new row; the old one is
-- history. The log itself isn't kept. One whose log couldn't be read, or
-- held nothing a reading knows, is "unclassified", with why, so status
-- says that once and nothing asks again.

CREATE TABLE ci_failures (
 repository_id TEXT NOT NULL,
 branch_id TEXT NOT NULL,
 head TEXT NOT NULL,
 run INTEGER NOT NULL,
 attempt INTEGER NOT NULL,
 job INTEGER NOT NULL,
 check_name TEXT NOT NULL,
 cause TEXT NOT NULL,
 file TEXT NOT NULL DEFAULT '',
 url TEXT NOT NULL DEFAULT '',
 status INTEGER NOT NULL DEFAULT 0,
 reason TEXT NOT NULL DEFAULT '',
 at INTEGER NOT NULL,
 FOREIGN KEY(repository_id, branch_id) REFERENCES branches(repository_id, id)
) STRICT;

CREATE UNIQUE INDEX ci_failure_key ON ci_failures(repository_id, branch_id, job, attempt);
CREATE INDEX ci_failure_head ON ci_failures(repository_id, branch_id, head);
