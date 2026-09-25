-- Schema 7: the maintainer loop (Design v3 §6.12 and §11). A branch says
-- who started it, a person or serve, since serve submits only its own; an
-- edit keeps what comparing the old and new upstream archives found, since
-- a finding holds an update for a person's look.
ALTER TABLE branches ADD COLUMN origin TEXT NOT NULL DEFAULT 'person' CHECK(origin IN ('person','serve'));
ALTER TABLE edits ADD COLUMN upstream TEXT NOT NULL DEFAULT '' CHECK(upstream = '' OR json_valid(upstream));
