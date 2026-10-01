-- Schema 27: a branch keeps a person's note for its pull request, which
-- the description gives under Description each time dockhand writes it
-- (submit --note). dockhand writes the whole description, so a person had
-- no way to say in it why rust's tests failed (the rust run,
-- macports/macports-ports#35084). Empty for none, and for every branch
-- before it.
ALTER TABLE branches ADD COLUMN note TEXT NOT NULL DEFAULT '';
