-- Schema 47: a branch keeps the Trac tickets update, ship, or bump's
-- --closes named (model.Branch.Closes, space-separated numbers), which
-- tidy writes as Closes: trailers with each ticket's URL (the docs dig's
-- I41). Empty for every branch before it.

ALTER TABLE branches ADD COLUMN closes TEXT NOT NULL DEFAULT '';
