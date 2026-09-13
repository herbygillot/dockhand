ALTER TABLE jobs ADD COLUMN resolved_release TEXT CHECK (resolved_release IS NULL OR json_valid(resolved_release));
PRAGMA user_version=4;
