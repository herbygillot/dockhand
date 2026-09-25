-- dockhand v3's first schema (docs/design-v3.md §3, §11). Times are Unix
-- milliseconds, UTC. Every record belongs to one registered repository.

CREATE TABLE repositories (
 id TEXT PRIMARY KEY,
 common_dir TEXT NOT NULL UNIQUE,
 created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE branches (
 repository_id TEXT NOT NULL REFERENCES repositories(id),
 id TEXT NOT NULL,
 name TEXT NOT NULL,
 base TEXT NOT NULL,
 worktree TEXT NOT NULL,
 managed INTEGER NOT NULL CHECK(managed IN (0,1)),
 title TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('open','merged','closed','archived')),
 pr_repository TEXT, pr_number INTEGER, pr_head TEXT,
 created_at INTEGER NOT NULL,
 PRIMARY KEY(repository_id, id),
 CHECK((pr_repository IS NULL) = (pr_number IS NULL) AND (pr_number IS NULL) = (pr_head IS NULL))
) STRICT;
-- A Git name belongs to at most one branch that is not merged; a merged
-- branch's name can be used again.
CREATE UNIQUE INDEX branch_name ON branches(repository_id, name) WHERE state <> 'merged';
CREATE UNIQUE INDEX branch_pull_request ON branches(repository_id, pr_repository, pr_number) WHERE pr_number IS NOT NULL;

CREATE TABLE revisions (
 repository_id TEXT NOT NULL,
 id TEXT NOT NULL,
 branch_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('commit','snapshot')),
 snapshot INTEGER NOT NULL,
 commit_id TEXT NOT NULL,
 tree_id TEXT NOT NULL,
 base_id TEXT NOT NULL,
 head_id TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 PRIMARY KEY(repository_id, id),
 FOREIGN KEY(repository_id, branch_id) REFERENCES branches(repository_id, id),
 CHECK(kind = 'commit' AND snapshot = 0 AND commit_id <> '' OR kind = 'snapshot' AND snapshot > 0 AND commit_id = '')
) STRICT;
CREATE UNIQUE INDEX revision_snapshot ON revisions(repository_id, branch_id, snapshot) WHERE kind = 'snapshot';

-- A plan is frozen at creation, so its structure is kept whole as JSON.
CREATE TABLE plans (
 repository_id TEXT NOT NULL,
 id TEXT NOT NULL,
 revision_id TEXT NOT NULL,
 body TEXT NOT NULL CHECK(json_valid(body)),
 created_at INTEGER NOT NULL,
 PRIMARY KEY(repository_id, id),
 FOREIGN KEY(repository_id, revision_id) REFERENCES revisions(repository_id, id)
) STRICT;

CREATE TABLE runs (
 repository_id TEXT NOT NULL,
 id TEXT NOT NULL,
 number INTEGER NOT NULL CHECK(number > 0),
 branch_id TEXT NOT NULL,
 revision_id TEXT NOT NULL,
 plan_id TEXT NOT NULL,
 origin TEXT NOT NULL CHECK(origin IN ('person','serve')),
 state TEXT NOT NULL CHECK(state IN ('queued','running','passed','failed','attention','canceled')),
 detail TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 finished_at INTEGER,
 PRIMARY KEY(repository_id, id),
 UNIQUE(repository_id, number),
 FOREIGN KEY(repository_id, branch_id) REFERENCES branches(repository_id, id),
 FOREIGN KEY(repository_id, revision_id) REFERENCES revisions(repository_id, id),
 FOREIGN KEY(repository_id, plan_id) REFERENCES plans(repository_id, id),
 CHECK((state IN ('passed','failed','attention','canceled')) = (finished_at IS NOT NULL))
) STRICT;
CREATE INDEX run_state ON runs(repository_id, state, number);
CREATE INDEX run_branch ON runs(repository_id, branch_id, number);

CREATE TABLE executions (
 repository_id TEXT NOT NULL,
 id TEXT NOT NULL,
 run_id TEXT NOT NULL,
 provider TEXT NOT NULL,
 platform_os TEXT NOT NULL,
 platform_version TEXT NOT NULL,
 platform_architecture TEXT NOT NULL,
 attempt INTEGER NOT NULL CHECK(attempt BETWEEN 1 AND 3),
 state TEXT NOT NULL CHECK(state IN ('waiting','running','finished','infrastructure-failed','canceled')),
 detail TEXT NOT NULL,
 provider_ref TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 finished_at INTEGER,
 PRIMARY KEY(repository_id, id),
 UNIQUE(repository_id, run_id, provider, platform_os, platform_version, platform_architecture, attempt),
 FOREIGN KEY(repository_id, run_id) REFERENCES runs(repository_id, id),
 CHECK((state IN ('finished','infrastructure-failed','canceled')) = (finished_at IS NOT NULL))
) STRICT;

CREATE TABLE results (
 repository_id TEXT NOT NULL,
 execution_id TEXT NOT NULL,
 target_id TEXT NOT NULL,
 outcome TEXT NOT NULL CHECK(outcome IN ('passed','failed','blocked','interrupted','not-run','unevaluated')),
 phase TEXT NOT NULL,
 tests TEXT NOT NULL,
 log TEXT NOT NULL,
 inputs TEXT NOT NULL,
 recorded_at INTEGER NOT NULL,
 PRIMARY KEY(repository_id, execution_id, target_id),
 FOREIGN KEY(repository_id, execution_id) REFERENCES executions(repository_id, id)
) STRICT;
-- A complete verdict is final, whatever the Go code does.
CREATE TRIGGER results_final BEFORE UPDATE ON results
 WHEN OLD.outcome IN ('passed','failed','blocked')
 BEGIN SELECT RAISE(ABORT, 'a complete target result is final'); END;

CREATE TABLE sessions (
 repository_id TEXT NOT NULL REFERENCES repositories(id),
 id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('serve','foreground','observer')),
 pid INTEGER NOT NULL CHECK(pid > 0),
 process_start TEXT NOT NULL,
 version TEXT NOT NULL,
 started_at INTEGER NOT NULL,
 heartbeat_at INTEGER NOT NULL,
 ended_at INTEGER,
 PRIMARY KEY(repository_id, id)
) STRICT;
CREATE INDEX session_live ON sessions(repository_id) WHERE ended_at IS NULL;

-- A lease row stays after release, so its generation keeps increasing.
CREATE TABLE leases (
 repository_id TEXT NOT NULL REFERENCES repositories(id),
 resource TEXT NOT NULL,
 holder TEXT,
 generation INTEGER NOT NULL CHECK(generation > 0),
 acquired_at INTEGER NOT NULL,
 PRIMARY KEY(repository_id, resource),
 FOREIGN KEY(repository_id, holder) REFERENCES sessions(repository_id, id)
) STRICT;
CREATE TRIGGER leases_fenced BEFORE UPDATE OF generation ON leases
 WHEN NEW.generation < OLD.generation
 BEGIN SELECT RAISE(ABORT, 'a lease generation never decreases'); END;

CREATE TABLE events (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 repository_id TEXT NOT NULL REFERENCES repositories(id),
 at INTEGER NOT NULL,
 session_id TEXT NOT NULL,
 branch_id TEXT NOT NULL,
 run_id TEXT NOT NULL,
 target_id TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind <> ''),
 level TEXT NOT NULL CHECK(level IN ('info','verbose','debug')),
 message TEXT NOT NULL
) STRICT;
CREATE INDEX event_repository ON events(repository_id, sequence);
