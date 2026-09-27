-- Schema 19: a result keeps each of its builders' parts, where a provider's
-- run has several: MacPorts' workflow has a runner for each macOS release,
-- and a port's result is theirs together. Each keeps its runner, outcome,
-- phase, tests, and log, and a runner that didn't build the port says so.
-- Empty for a provider with one builder, and for results made before it.
ALTER TABLE results ADD COLUMN builders TEXT NOT NULL DEFAULT '';
