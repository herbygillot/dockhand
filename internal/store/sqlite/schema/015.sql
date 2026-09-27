-- Schema 15: an execution keeps its environment's identity by origin as
-- it was when the execution began (decision 28): what the environment was
-- made from and with, such as a Tart image's source by digest, the setup
-- that made it, its tools and MacPorts, and the guest program's protocol.
-- Evidence compares it with the environment's identity now. Empty for
-- executions made before it, and where the provider can't say.
ALTER TABLE executions ADD COLUMN identity TEXT NOT NULL DEFAULT '';
