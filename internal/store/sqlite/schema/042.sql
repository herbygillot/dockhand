-- Schema 42: an assessment records the dockhand build that made it
-- (model.Assessment.Build), so an incomplete one another build made is
-- made again, where a recognizer fixed without a policy raise left the
-- old refusal standing until the tree moved (the docs dig's I26). Empty
-- for every assessment recorded before, which reads as another build's.

ALTER TABLE assessments ADD COLUMN build TEXT NOT NULL DEFAULT '';
