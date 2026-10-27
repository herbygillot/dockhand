-- Schema 45: a passed result keeps the files its build installed, as port
-- contents lists them, and its own dynamic libraries, each with its
-- install name and compatibility version (model.TargetResult.Installed,
-- Libraries, as JSON), which a check of the port's next version compares
-- (Then's item 9). NULL where its provider couldn't read them.

ALTER TABLE results ADD COLUMN installed TEXT;
ALTER TABLE results ADD COLUMN libraries TEXT;
