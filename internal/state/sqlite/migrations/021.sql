ALTER TABLE pull_requests ADD COLUMN observe_after INTEGER;
CREATE INDEX pull_requests_due ON pull_requests(repository_id,observe_after);
PRAGMA user_version=21;
