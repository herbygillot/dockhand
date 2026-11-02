-- Schema 56: a reused result keeps how it stands for a revision whose port
-- directories differ from what its build read only in options no build
-- reads (model.TargetResult.Carried, as JSON, NULL for every other
-- result): the ports judged with the options changed, the check and commit
-- it was built at, and whether lint ran again on this Portfile, which the
-- pull request's Tested on and status say.

ALTER TABLE results ADD COLUMN carried TEXT CHECK(carried IS NULL OR json_valid(carried));
