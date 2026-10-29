-- Schema 50: rename and move are authoring edits (engine.RenamePort), a
-- port renamed with an obsolete stub left at its old name, or moved to
-- another category's directory. A CHECK constraint can't be altered, so
-- edits is rebuilt to allow them, as schema 8 did for create.
CREATE TABLE edits_50 (
 repository_id TEXT NOT NULL,
 id TEXT NOT NULL,
 branch_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('update','checksums','revbump','create','rename','move')),
 port TEXT NOT NULL,
 directory TEXT NOT NULL,
 subject TEXT NOT NULL,
 files TEXT NOT NULL CHECK(json_valid(files)),
 at INTEGER NOT NULL,
 upstream TEXT NOT NULL DEFAULT '' CHECK(upstream = '' OR json_valid(upstream)),
 release TEXT NOT NULL DEFAULT '',
 excepted TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(repository_id, id),
 FOREIGN KEY(repository_id, branch_id) REFERENCES branches(repository_id, id)
) STRICT;
INSERT INTO edits_50 SELECT repository_id, id, branch_id, kind, port, directory, subject, files, at, upstream, release, excepted FROM edits;
DROP TABLE edits;
ALTER TABLE edits_50 RENAME TO edits;
CREATE INDEX edit_branch ON edits(repository_id, branch_id, at);
