ALTER TABLE jobs ADD COLUMN consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK(consecutive_failures>=0);
ALTER TABLE attempts ADD COLUMN consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK(consecutive_failures>=0);
ALTER TABLE resources ADD COLUMN consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK(consecutive_failures>=0);
ALTER TABLE publications ADD COLUMN write_refusals INTEGER NOT NULL DEFAULT 0 CHECK(write_refusals>=0);
PRAGMA user_version=14;
