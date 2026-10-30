-- Schema 52: an update's edit keeps the patch problems an unattended ship
-- held it before its check for (model.Edit.Held), as JSON, so serve's
-- recovery leaves it held (Codex's review of 275125ff, finding 2). Empty
-- for every other edit, and for those made before it.

ALTER TABLE edits ADD COLUMN held TEXT NOT NULL DEFAULT '' CHECK(held = '' OR json_valid(held));
