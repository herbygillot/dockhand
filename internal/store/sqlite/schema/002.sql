-- Schema 2: what tidy and submit keep (Design v3 §8, §9).

-- The pull request's publication state: the commit dockhand last pushed
-- and the description it last wrote.
ALTER TABLE branches ADD COLUMN pr_pushed TEXT NOT NULL DEFAULT '';
ALTER TABLE branches ADD COLUMN pr_body TEXT NOT NULL DEFAULT '';
ALTER TABLE branches ADD COLUMN pr_draft INTEGER NOT NULL DEFAULT 0 CHECK(pr_draft IN (0,1));

-- What authoring commands wrote, file by file, oldest first.
CREATE TABLE edits (
 repository_id TEXT NOT NULL,
 id TEXT NOT NULL,
 branch_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('update','checksums')),
 port TEXT NOT NULL,
 directory TEXT NOT NULL,
 subject TEXT NOT NULL,
 files TEXT NOT NULL CHECK(json_valid(files)),
 at INTEGER NOT NULL,
 PRIMARY KEY(repository_id, id),
 FOREIGN KEY(repository_id, branch_id) REFERENCES branches(repository_id, id)
) STRICT;
CREATE INDEX edit_branch ON edits(repository_id, branch_id, at);

CREATE TABLE checkpoints (
 repository_id TEXT NOT NULL,
 number INTEGER NOT NULL CHECK(number > 0),
 branch_id TEXT NOT NULL,
 before_head TEXT NOT NULL,
 after_head TEXT NOT NULL,
 at INTEGER NOT NULL,
 restored_at INTEGER,
 PRIMARY KEY(repository_id, number),
 FOREIGN KEY(repository_id, branch_id) REFERENCES branches(repository_id, id)
) STRICT;

CREATE TABLE acceptances (
 repository_id TEXT NOT NULL,
 branch_id TEXT NOT NULL,
 commit_id TEXT NOT NULL,
 port TEXT NOT NULL,
 at INTEGER NOT NULL,
 PRIMARY KEY(repository_id, branch_id, commit_id, port),
 FOREIGN KEY(repository_id, branch_id) REFERENCES branches(repository_id, id)
) STRICT;
