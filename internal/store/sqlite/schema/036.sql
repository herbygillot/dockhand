-- Schema 36: the dependency whose failure failed a target at its
-- dependencies phase, and its version and revision as the ports tree had
-- them, where the provider said, for an acceptance of that failure that
-- stands until either changes, or the image does (v0.5.0 part 2, item 2;
-- the person's choice, 2026-10-18). Empty for every other result, and for
-- one recorded before.

ALTER TABLE results ADD COLUMN dependency TEXT NOT NULL DEFAULT '';
ALTER TABLE results ADD COLUMN dependency_version TEXT NOT NULL DEFAULT '';
