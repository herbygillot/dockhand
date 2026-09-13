CREATE TABLE provider_pools (
 id TEXT PRIMARY KEY, scope TEXT NOT NULL UNIQUE, directory TEXT NOT NULL UNIQUE,
 capacity INTEGER NOT NULL CHECK(capacity > 0)
) STRICT;
CREATE TABLE provider_executions (
 id TEXT PRIMARY KEY, pool_id TEXT NOT NULL REFERENCES provider_pools(id),
 repository_id TEXT NOT NULL REFERENCES repositories(id), attempt_id TEXT,
 resource TEXT, payload BLOB, state TEXT NOT NULL CHECK(state IN ('reserved','admitted','closed','released')),
 occupied INTEGER NOT NULL CHECK(occupied IN (0,1)), result BLOB, created_at INTEGER NOT NULL,
 UNIQUE(pool_id,resource),
 CHECK(state NOT IN ('closed','released') OR occupied=0),
    CHECK(state!='reserved' OR occupied=1),
    CHECK(state!='admitted' OR occupied=1 OR result IS NOT NULL),
 CHECK(state NOT IN ('reserved','admitted') OR (attempt_id IS NOT NULL AND resource IS NOT NULL AND payload IS NOT NULL)),
 FOREIGN KEY(repository_id,attempt_id) REFERENCES attempts(repository_id,id)
) STRICT;
CREATE INDEX provider_occupied ON provider_executions(pool_id,id) WHERE occupied=1;
