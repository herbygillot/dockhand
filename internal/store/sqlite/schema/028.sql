-- Schema 28: when a branch left open, merged, closed, or archived, from
-- which cleanup times what it keeps of the branch's checks (D6); NULL
-- while it's open. A branch that ended before this schema is taken to
-- have ended now, so what it recorded is kept cleanup.after from the
-- migration, never removed at once.
ALTER TABLE branches ADD COLUMN ended_at INTEGER;
UPDATE branches SET ended_at = CAST(strftime('%s','now') AS INTEGER) * 1000 WHERE state <> 'open';
