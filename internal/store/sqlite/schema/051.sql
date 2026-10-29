-- Schema 51: an update's or a checksums refresh's edit keeps the
-- plain-HTTP links it switched to https (model.Edit.HTTPS), as JSON, which
-- tidy's commit and the pull request's description say. Empty for every
-- other edit, and for those made before it.

ALTER TABLE edits ADD COLUMN https TEXT NOT NULL DEFAULT '' CHECK(https = '' OR json_valid(https));
