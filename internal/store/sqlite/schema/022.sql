-- Schema 22: the archives dockhand keeps of what checks built (decision
-- 28), for later builds to install rather than build again. A row says an
-- archive is kept: its file, named by its digest beside the database, is
-- whole and matches the digest its build reported, which is when it is
-- ready for dependents (decision 44). Results name their archives by
-- digest, and cleanup keeps every archive a live result names.
CREATE TABLE archives (
 repository_id TEXT NOT NULL REFERENCES repositories(id),
 digest TEXT NOT NULL,
 name TEXT NOT NULL,
 size INTEGER NOT NULL,
 kept_at INTEGER NOT NULL,
 PRIMARY KEY(repository_id, digest)
) STRICT;
