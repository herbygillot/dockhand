-- Schema 3: a cancel is recorded on its run, and whoever holds the run
-- applies it (Design v3 §11).
ALTER TABLE runs ADD COLUMN cancel_requested_at INTEGER;
