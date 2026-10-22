-- Schema 33: the schema each process expects, for status's list of the
-- dockhand processes running (roadmap item 7). A session records the
-- newest schema its build writes; sessions recorded before this one say 0,
-- an earlier build's.

ALTER TABLE sessions ADD COLUMN schema INTEGER NOT NULL DEFAULT 0;
