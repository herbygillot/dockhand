-- Schema 59: a branch keeps the person's statements for the pull request
-- template's last two items, that they tested its binaries and its
-- variants (model.Branch.TestedBinaries, TestedVariants), as it keeps
-- their note: a later run with --yes had to say them again (#91).

ALTER TABLE branches ADD COLUMN tested TEXT NOT NULL DEFAULT '';
