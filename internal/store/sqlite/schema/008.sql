-- Schema 8: create is an authoring edit (Design v3 §6.4), the new port's
-- first Portfile. A CHECK constraint can't be altered, so edits is rebuilt
-- to allow it.
CREATE TABLE edits_8 (
 repository_id TEXT NOT NULL,
 id TEXT NOT NULL,
 branch_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('update','checksums','revbump','create')),
 port TEXT NOT NULL,
 directory TEXT NOT NULL,
 subject TEXT NOT NULL,
 files TEXT NOT NULL CHECK(json_valid(files)),
 at INTEGER NOT NULL,
 upstream TEXT NOT NULL DEFAULT '' CHECK(upstream = '' OR json_valid(upstream)),
 PRIMARY KEY(repository_id, id),
 FOREIGN KEY(repository_id, branch_id) REFERENCES branches(repository_id, id)
) STRICT;
INSERT INTO edits_8 SELECT repository_id, id, branch_id, kind, port, directory, subject, files, at, upstream FROM edits;
DROP TABLE edits;
ALTER TABLE edits_8 RENAME TO edits;
CREATE INDEX edit_branch ON edits(repository_id, branch_id, at);
