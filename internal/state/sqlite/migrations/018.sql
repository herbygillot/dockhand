ALTER TABLE jobs ADD COLUMN consecutive_waits INTEGER NOT NULL DEFAULT 0 CHECK(consecutive_waits>=0);
ALTER TABLE attempts ADD COLUMN consecutive_waits INTEGER NOT NULL DEFAULT 0 CHECK(consecutive_waits>=0);
ALTER TABLE resources ADD COLUMN consecutive_waits INTEGER NOT NULL DEFAULT 0 CHECK(consecutive_waits>=0);
PRAGMA user_version=18;
