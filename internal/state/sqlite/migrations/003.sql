PRAGMA defer_foreign_keys=ON;
ALTER TABLE jobs ADD COLUMN claim_owner TEXT;
ALTER TABLE jobs ADD COLUMN claim_generation INTEGER NOT NULL DEFAULT 0 CHECK(claim_generation>=0);
ALTER TABLE jobs ADD COLUMN claim_until INTEGER;
ALTER TABLE jobs ADD COLUMN retry_at INTEGER;
ALTER TABLE jobs ADD COLUMN prepared TEXT CHECK(prepared IS NULL OR json_valid(prepared));
CREATE TABLE plans_new (
 repository_id TEXT NOT NULL, job_id TEXT PRIMARY KEY, revision_id TEXT,
 targets TEXT NOT NULL CHECK(json_valid(targets)),
 FOREIGN KEY(repository_id,job_id) REFERENCES jobs(repository_id,id),
 FOREIGN KEY(repository_id,revision_id) REFERENCES revisions(repository_id,id)
) STRICT;
INSERT INTO plans_new SELECT * FROM plans;
DROP TABLE plans;
ALTER TABLE plans_new RENAME TO plans;
CREATE TABLE attempts_new (
 id TEXT PRIMARY KEY, repository_id TEXT NOT NULL, job_id TEXT NOT NULL, target_id TEXT NOT NULL,
 revision_id TEXT, source_id TEXT NOT NULL, build TEXT NOT NULL CHECK(json_valid(build)),
 state TEXT NOT NULL CHECK(state IN ('queued','submitting','running','finished','uncertain','canceled')),
 claim_owner TEXT, claim_generation INTEGER NOT NULL CHECK(claim_generation>=0), claim_until INTEGER,
 retry_at INTEGER, next_action_at INTEGER, cancel_sent_at INTEGER, cancel_observe INTEGER NOT NULL CHECK(cancel_observe IN (0,1)),
 last_error TEXT NOT NULL, created_at INTEGER NOT NULL,
 UNIQUE(repository_id,id), UNIQUE(repository_id,job_id,id),
 CHECK((claim_owner IS NULL)=(claim_until IS NULL)),
 FOREIGN KEY(repository_id,job_id) REFERENCES jobs(repository_id,id),
 FOREIGN KEY(repository_id,revision_id) REFERENCES revisions(repository_id,id),
 FOREIGN KEY(repository_id,source_id) REFERENCES sources(repository_id,id)
) STRICT;
INSERT INTO attempts_new SELECT * FROM attempts;
DROP TABLE attempts;
ALTER TABLE attempts_new RENAME TO attempts;
CREATE INDEX attempts_job ON attempts(repository_id,job_id,id);
CREATE INDEX attempts_due ON attempts(repository_id,next_action_at,id) WHERE next_action_at IS NOT NULL;
