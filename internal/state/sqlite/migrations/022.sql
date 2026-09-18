PRAGMA defer_foreign_keys=ON;
CREATE TABLE publications_new (
 id TEXT PRIMARY KEY, repository_id TEXT NOT NULL, job_id TEXT NOT NULL UNIQUE,
 change_id TEXT NOT NULL, revision_id TEXT NOT NULL, evidence_attempt TEXT,
 forge TEXT NOT NULL, head_repository TEXT NOT NULL COLLATE NOCASE, head_branch TEXT NOT NULL,
 spec TEXT NOT NULL CHECK(json_valid(spec)),
 state TEXT NOT NULL CHECK(state IN ('pending','applying','uncertain','confirmed','needs-attention')),
 push_started INTEGER NOT NULL CHECK(push_started IN (0,1)), write_started INTEGER NOT NULL CHECK(write_started IN (0,1)),
 confirmed_at INTEGER, last_error TEXT NOT NULL,
 write_refusals INTEGER NOT NULL DEFAULT 0 CHECK(write_refusals>=0),
 CHECK(write_started<=push_started), CHECK((state='confirmed')=(confirmed_at IS NOT NULL)),
 CHECK((evidence_attempt IS NULL)=(json_extract(spec,'$.unverified') IS TRUE)),
 FOREIGN KEY(repository_id,job_id) REFERENCES jobs(repository_id,id),
 FOREIGN KEY(repository_id,change_id,revision_id) REFERENCES revisions(repository_id,change_id,id),
 FOREIGN KEY(repository_id,evidence_attempt) REFERENCES attempts(repository_id,id)
) STRICT;
INSERT INTO publications_new SELECT * FROM publications;
DROP TABLE publications;
ALTER TABLE publications_new RENAME TO publications;
CREATE UNIQUE INDEX publication_active_head ON publications(forge,head_repository,head_branch) WHERE state IN ('pending','applying','uncertain');
PRAGMA user_version=22;
