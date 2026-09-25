-- Schema 4: revision bumps are authoring edits, rebase keeps checkpoints
-- as tidy does, and a pull request's observed state is kept.

-- A CHECK constraint can't be altered, so edits is rebuilt to allow
-- revbump.
CREATE TABLE edits_4 (
 repository_id TEXT NOT NULL,
 id TEXT NOT NULL,
 branch_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('update','checksums','revbump')),
 port TEXT NOT NULL,
 directory TEXT NOT NULL,
 subject TEXT NOT NULL,
 files TEXT NOT NULL CHECK(json_valid(files)),
 at INTEGER NOT NULL,
 PRIMARY KEY(repository_id, id),
 FOREIGN KEY(repository_id, branch_id) REFERENCES branches(repository_id, id)
) STRICT;
INSERT INTO edits_4 SELECT repository_id, id, branch_id, kind, port, directory, subject, files, at FROM edits;
DROP TABLE edits;
ALTER TABLE edits_4 RENAME TO edits;
CREATE INDEX edit_branch ON edits(repository_id, branch_id, at);

ALTER TABLE checkpoints ADD COLUMN kind TEXT NOT NULL DEFAULT 'tidy' CHECK(kind IN ('tidy','rebase'));

-- What the forge last reported about the branch's pull request, as JSON.
ALTER TABLE branches ADD COLUMN pr_observed TEXT NOT NULL DEFAULT '' CHECK(pr_observed = '' OR json_valid(pr_observed));
