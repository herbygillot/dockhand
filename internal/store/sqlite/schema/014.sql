-- Schema 14: a checkpoint is recorded before the Git change it keeps, as
-- prepared, and becomes applied once the change is made, or abandoned
-- when it wasn't. A process that stops between the two leaves a prepared
-- checkpoint the next one finishes from what Git shows (Design v3 §8).
-- Checkpoints recorded before it were recorded after their change.
ALTER TABLE checkpoints ADD COLUMN state TEXT NOT NULL DEFAULT 'applied';
