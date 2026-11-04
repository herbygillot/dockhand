-- Schema 58: a run keeps the shells opened in its guests while it ran
-- (model.Run.Entered, as JSON, NULL for a run none entered): a result on
-- that release recorded after one opened isn't evidence.

ALTER TABLE runs ADD COLUMN entered TEXT CHECK(entered IS NULL OR json_valid(entered));
