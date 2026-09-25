-- Schema 9: a checkpoint keeps the index it replaced, whose tree can hold
-- staged content neither the old head nor the working files have
-- (Design v3 §8). Empty for checkpoints made before it, and for a rebase,
-- which only runs with nothing uncommitted.
ALTER TABLE checkpoints ADD COLUMN index_tree TEXT NOT NULL DEFAULT '';
