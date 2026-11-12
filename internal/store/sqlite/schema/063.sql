-- Schema 63: what schema 62's launch notes left unsaid (Codex's review of
-- #193 steps 5 and 6, findings 3 and 6). An attempt that was unfinished
-- when dockhand began noting launches is marked as one whose launch was
-- never noted (model.LaunchUnrecorded), so a stop reads it by its
-- reference, or says it isn't known to have stopped, where an empty note
-- since means nothing was started. And the time a run's providers were
-- last asked to stop its work, which every process reads before asking
-- again.

UPDATE executions SET launch = 'unrecorded: started before dockhand noted launches'
 WHERE launch = '' AND EXISTS (SELECT 1 FROM runs WHERE runs.repository_id = executions.repository_id AND runs.id = executions.run_id AND runs.state IN ('queued', 'running'));

ALTER TABLE runs ADD COLUMN stop_asked INTEGER;
