CREATE INDEX jobs_change ON jobs(repository_id,change_id,id);
PRAGMA user_version=10;
