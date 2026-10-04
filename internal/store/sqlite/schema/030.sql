-- Schema 30: the archives a guest installed its targets' dependencies
-- from, kept by digest beside the targets' own (batch 90): rust and cargo,
-- built from source where MacPorts had no archive for the release yet, so
-- a later check's guest installs them rather than build them again. Each
-- is kept for the port it is and the environment its guest was; the
-- archive itself is the archives row, which pruning forgets with these.

CREATE TABLE dependency_archives (
 repository_id TEXT NOT NULL,
 digest TEXT NOT NULL,
 port TEXT NOT NULL CHECK(port <> ''),
 provider TEXT NOT NULL,
 platform_os TEXT NOT NULL,
 platform_version TEXT NOT NULL,
 platform_architecture TEXT NOT NULL,
 developer_tools TEXT NOT NULL,
 kept_at INTEGER NOT NULL,
 PRIMARY KEY(repository_id, digest, port, provider, platform_os, platform_version, platform_architecture, developer_tools),
 FOREIGN KEY(repository_id, digest) REFERENCES archives(repository_id, digest) ON DELETE CASCADE
) STRICT;

CREATE INDEX dependency_archives_by_port ON dependency_archives(repository_id, port, provider, platform_os, platform_version, platform_architecture, developer_tools, kept_at);
