-- Schema 16: what each target's build read (decision 28), by content. A
-- result names its inputs by key, and the record is kept once for every
-- build that read the same: the environment's identity, the target's
-- directory and _resources by tree, the variants asked for, and the ports
-- active as it built, each with its archive's digest. A result also keeps
-- the digest of the archive its build made. Empty for results recorded
-- before it, and where the provider couldn't say.
CREATE TABLE inputs (
 repository_id TEXT NOT NULL REFERENCES repositories(id),
 key TEXT NOT NULL,
 record TEXT NOT NULL,
 PRIMARY KEY(repository_id, key)
) STRICT;
ALTER TABLE results ADD COLUMN archive TEXT NOT NULL DEFAULT '';
