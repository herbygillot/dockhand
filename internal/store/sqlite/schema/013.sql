-- Schema 13: a checkpoint keeps the master the branch started from before
-- and after it, so restoring a rebase puts the branch's base back with its
-- history (Design v3 §8). A tidy doesn't move it, so the two are equal.
-- Empty for checkpoints made before it.
ALTER TABLE checkpoints ADD COLUMN base_before TEXT NOT NULL DEFAULT '';
ALTER TABLE checkpoints ADD COLUMN base_after TEXT NOT NULL DEFAULT '';
