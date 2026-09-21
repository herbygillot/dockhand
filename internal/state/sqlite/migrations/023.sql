ALTER TABLE changes ADD COLUMN keep_body INTEGER NOT NULL DEFAULT 0 CHECK(keep_body IN (0,1));
PRAGMA user_version=23;
