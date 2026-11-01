-- Schema 53: a run keeps when it took its slot, the lease its driver
-- holds (model.Run.StartedAt), which logs and status's queue say beside
-- its queued and finished times, with how long it waited. Null for a run
-- queued still, and for those recorded before it.

ALTER TABLE runs ADD COLUMN started_at INTEGER;
