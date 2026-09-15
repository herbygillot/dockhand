PRAGMA defer_foreign_keys=ON;
CREATE TABLE submissions_new (
 id TEXT PRIMARY KEY, repository_id TEXT NOT NULL, attempt_id TEXT NOT NULL, sequence INTEGER NOT NULL CHECK(sequence>0),
 provider TEXT NOT NULL, run_id TEXT, created_at INTEGER NOT NULL, admitted_at INTEGER, closed_at INTEGER,
 UNIQUE(repository_id,id), UNIQUE(repository_id,attempt_id,id), UNIQUE(attempt_id,sequence),
 CHECK(closed_at IS NULL OR run_id IS NULL),
 FOREIGN KEY(repository_id,attempt_id) REFERENCES attempts(repository_id,id)
) STRICT;
INSERT INTO submissions_new SELECT * FROM submissions;
DROP TABLE submissions;
ALTER TABLE submissions_new RENAME TO submissions;
CREATE UNIQUE INDEX live_submission ON submissions(attempt_id) WHERE closed_at IS NULL;
