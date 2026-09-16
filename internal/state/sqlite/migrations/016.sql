CREATE INDEX resources_diagnostics ON resources(repository_id,released_at,id) WHERE state='released' AND artifacts_pruned_at IS NULL;
PRAGMA user_version=16;
