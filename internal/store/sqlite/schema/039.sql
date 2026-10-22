-- Schema 39: what a passed build's installed Mach-O files link under the
-- prefix, each library with the binary that links it and the port that
-- provides it (v0.5.0 part 6), as JSON: NULL where its provider couldn't
-- read them, and an empty list where nothing links. An observation of the
-- build, beside its result, never a reuse key.

ALTER TABLE results ADD COLUMN links TEXT;
