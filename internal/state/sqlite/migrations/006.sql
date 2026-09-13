CREATE TABLE pull_requests (
 id TEXT PRIMARY KEY, repository_id TEXT NOT NULL, change_id TEXT NOT NULL,
 forge TEXT NOT NULL, remote_repository TEXT NOT NULL COLLATE NOCASE, number INTEGER NOT NULL CHECK(number>0),
 observation TEXT NOT NULL CHECK(json_valid(observation)),
 UNIQUE(repository_id,id), UNIQUE(repository_id,change_id), UNIQUE(repository_id,forge,remote_repository,number),
 FOREIGN KEY(repository_id,change_id) REFERENCES changes(repository_id,id)
) STRICT;
ALTER TABLE changes ADD COLUMN published_revision TEXT REFERENCES revisions(id);
ALTER TABLE changes ADD COLUMN pull_request_id TEXT REFERENCES pull_requests(id);
CREATE TABLE publications (
 id TEXT PRIMARY KEY, repository_id TEXT NOT NULL, job_id TEXT NOT NULL UNIQUE,
 change_id TEXT NOT NULL, revision_id TEXT NOT NULL, evidence_attempt TEXT NOT NULL,
 forge TEXT NOT NULL, head_repository TEXT NOT NULL COLLATE NOCASE, head_branch TEXT NOT NULL,
 spec TEXT NOT NULL CHECK(json_valid(spec)),
 state TEXT NOT NULL CHECK(state IN ('pending','applying','uncertain','confirmed','needs-attention')),
 push_started INTEGER NOT NULL CHECK(push_started IN (0,1)), write_started INTEGER NOT NULL CHECK(write_started IN (0,1)),
 confirmed_at INTEGER, last_error TEXT NOT NULL,
 CHECK(write_started<=push_started), CHECK((state='confirmed')=(confirmed_at IS NOT NULL)),
 FOREIGN KEY(repository_id,job_id) REFERENCES jobs(repository_id,id),
 FOREIGN KEY(repository_id,change_id,revision_id) REFERENCES revisions(repository_id,change_id,id),
 FOREIGN KEY(repository_id,evidence_attempt) REFERENCES attempts(repository_id,id)
) STRICT;
CREATE UNIQUE INDEX publication_active_head ON publications(forge,head_repository,head_branch) WHERE state IN ('pending','applying','uncertain');
PRAGMA user_version=6;
