PRAGMA defer_foreign_keys=ON;
CREATE TABLE provider_pools_new (
 id TEXT PRIMARY KEY, scope TEXT NOT NULL UNIQUE, directory TEXT NOT NULL,
 capacity INTEGER NOT NULL CHECK(capacity > 0)
) STRICT;
INSERT INTO provider_pools_new SELECT id,scope,directory,capacity FROM provider_pools;
DROP TABLE provider_pools;
ALTER TABLE provider_pools_new RENAME TO provider_pools;
