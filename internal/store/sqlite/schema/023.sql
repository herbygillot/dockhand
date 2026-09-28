-- Schema 23: indexes for the questions dockhand asks of its history
-- (docs/reviews/2026-09-28-sql-review.md, findings 1, 2, 4, and 9). Each
-- query read every row of its table in the repository, a cost that grew
-- with every check kept: pruning archives read every result once for each
-- archive. The indexes are plain rather than partial, so a query uses its
-- index however its conditions are worded.

-- Which results name an archive, for pruning archives.
CREATE INDEX result_archive ON results(repository_id, archive);
-- A target's results, newest last, for reuse.
CREATE INDEX result_target ON results(repository_id, target_id, recorded_at);
-- A branch's revisions, oldest first.
CREATE INDEX revision_branch ON revisions(repository_id, branch_id, created_at);
-- The executions a provider knows by a reference, such as a clone's name.
CREATE INDEX execution_ref ON executions(repository_id, provider_ref);
-- The events of a kind since a time, such as serve's submissions today.
CREATE INDEX event_kind ON events(repository_id, kind, at);
-- A branch's checkpoints, in order.
CREATE INDEX checkpoint_branch ON checkpoints(repository_id, branch_id, number);

-- Nothing asks for the sessions that haven't ended.
DROP INDEX session_live;
