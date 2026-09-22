ALTER TABLE revisions ADD COLUMN shared TEXT NOT NULL DEFAULT 'null';
PRAGMA user_version=24;
