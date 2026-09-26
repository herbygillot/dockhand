-- Schema 10: a result says why a target wasn't run, when its provider
-- knows: Tart, a target that needs Xcode on a release with no Xcode image.
-- Empty for results recorded before it, and for every other outcome.
ALTER TABLE results ADD COLUMN detail TEXT NOT NULL DEFAULT '';
