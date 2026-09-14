ALTER TABLE resources ADD COLUMN artifacts_pruned_at INTEGER;
CREATE INDEX resources_retention ON resources(repository_id,state,id) WHERE artifacts_pruned_at IS NULL;
PRAGMA user_version=8;
