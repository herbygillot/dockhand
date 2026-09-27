-- Schema 18: an update's edit keeps the release it chose, as found: its
-- forge and repository, tag and upstream commit, or the distfiles it came
-- from (model.Release), for status to say where a version came from after
-- the update's own process is gone. Empty for other edits, and for edits
-- made before it.
ALTER TABLE edits ADD COLUMN release TEXT NOT NULL DEFAULT '';
