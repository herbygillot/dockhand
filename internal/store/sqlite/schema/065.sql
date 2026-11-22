-- Schema 65: an edit is recorded before its files are written (#194). An
-- edit gains the time its files were seen written, zero until then, and
-- what preparing it found and did, as JSON; its files' modes are in the
-- files JSON already. A hold is no longer stored: an edit from before
-- with one keeps it as a patch-rejected finding, left, its held lines the
-- detail, which nothing later compares with. Every edit from before was
-- written, at its own time. A branch gains what was asked of its work,
-- as JSON, and the name a rename is moving it to, empty but while one is
-- under way.
ALTER TABLE edits ADD COLUMN written INTEGER NOT NULL DEFAULT 0;
ALTER TABLE edits ADD COLUMN result TEXT NOT NULL DEFAULT '' CHECK(result = '' OR json_valid(result));
UPDATE edits SET written = at;
UPDATE edits SET result = json_object('Findings', json_array(json_object('Kind', 'patch-rejected', 'Detail', json(held)))) WHERE held != '';
ALTER TABLE edits DROP COLUMN held;
ALTER TABLE branches ADD COLUMN request TEXT NOT NULL DEFAULT '' CHECK(request = '' OR json_valid(request));
ALTER TABLE branches ADD COLUMN rename_to TEXT NOT NULL DEFAULT '';
