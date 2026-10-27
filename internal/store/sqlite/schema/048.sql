-- Schema 48: a result keeps what port lint warned of
-- (model.TargetResult.Lint, as JSON, NULL where not read) and whether lint
-- ran with --nitpick, as for a port the branch adds (the docs dig's I13).

ALTER TABLE results ADD COLUMN lint TEXT;
ALTER TABLE results ADD COLUMN nitpicked INTEGER NOT NULL DEFAULT 0;
