-- Schema 43: a revision bump's edit keeps the dependents --except left
-- out of the same rebuild (model.Edit.Excepted), space-separated, which
-- the pull request's description lists for a reviewer to disagree with
-- (the docs dig's I41). Empty for every other edit, and for those made
-- before it.

ALTER TABLE edits ADD COLUMN excepted TEXT NOT NULL DEFAULT '';
