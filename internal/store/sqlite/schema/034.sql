-- Schema 34: forgetting an archived branch, and what clean kept (roadmap
-- item 8). A branch clean --delete forgets keeps a record, since other
-- branches' results name its executions in results.reused_from, which is
-- no foreign key, but nothing reads it as a branch again: forgotten_at says
-- when, and its name and pull request are free for another branch.
-- clean_kept says what clean kept of a cleaned branch, as "fork branch
-- kept: moved after merge", which status --all says.

ALTER TABLE branches ADD COLUMN forgotten_at INTEGER;
ALTER TABLE branches ADD COLUMN clean_kept TEXT NOT NULL DEFAULT '';

DROP INDEX branch_name;
CREATE UNIQUE INDEX branch_name ON branches(repository_id, name) WHERE state <> 'merged' AND forgotten_at IS NULL;
DROP INDEX branch_pull_request;
CREATE UNIQUE INDEX branch_pull_request ON branches(repository_id, pr_repository, pr_number) WHERE pr_number IS NOT NULL AND forgotten_at IS NULL;
