-- Schema 46: a passed result keeps its configure phase's summary lines
-- (model.TargetResult.Configure, as JSON), autoconf's "checking for",
-- CMake's "-- Found", meson's "Dependency … found", which a check of the
-- port's next version diffs (Then's item 10). NULL where not read.

ALTER TABLE results ADD COLUMN configure TEXT;
