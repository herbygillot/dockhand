-- Schema 12: an execution keeps what its environment reported about itself
-- (model.Observed, as JSON): the macOS and build versions, the architecture,
-- and the developer tools' versions, which a pull request's Tested on
-- states. Empty for executions made before it, and where the provider
-- can't say.
ALTER TABLE executions ADD COLUMN observed TEXT NOT NULL DEFAULT '';
