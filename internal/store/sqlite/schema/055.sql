-- Schema 55: an update's edit keeps the move it made of a GitHub port's
-- source (model.Edit.Source), as JSON: from GitHub's tarball, its archive,
-- or a Git clone to a release's own file or GitHub's archive, with the
-- asset and the submodules it carries, which tidy's commit and the pull
-- request's description say. Empty for every other edit, and for those
-- made before it.

ALTER TABLE edits ADD COLUMN source TEXT NOT NULL DEFAULT '' CHECK(source = '' OR json_valid(source));
