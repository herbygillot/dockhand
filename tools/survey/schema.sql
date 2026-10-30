-- The survey's record, one file per snapshot of the tree: facts, not
-- answers, each question a query (queries/). Every row of a run carries
-- its mode through run.
CREATE TABLE schema (version INTEGER NOT NULL);
INSERT INTO schema (version) VALUES (1);

CREATE TABLE run (
    id INTEGER PRIMARY KEY,
    mode TEXT NOT NULL,
    tree TEXT NOT NULL,
    "commit" TEXT NOT NULL,
    tclsh TEXT,
    portindex TEXT,
    base_version TEXT,
    host_platform TEXT,
    parallel INTEGER,
    started TEXT NOT NULL,
    finished TEXT,
    wall_seconds REAL,
    user_seconds REAL,
    system_seconds REAL,
    cores REAL,
    load_at_start TEXT,
    recording INTEGER NOT NULL,
    ports INTEGER
);

CREATE TABLE port (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    portfile TEXT,
    category TEXT
);

-- One port in one context: its platform and developer tools, modelled or
-- the Mac's own, and whether it was observed, refused, or failed.
CREATE TABLE evaluation (
    id INTEGER PRIMARY KEY,
    run_id INTEGER NOT NULL REFERENCES run (id),
    port_id INTEGER NOT NULL REFERENCES port (id),
    platform_os TEXT NOT NULL,
    platform_version TEXT NOT NULL,
    platform_arch TEXT NOT NULL,
    developer_tools TEXT NOT NULL DEFAULT '',
    modeled INTEGER NOT NULL,
    outcome TEXT NOT NULL,
    error TEXT,
    UNIQUE (run_id, port_id, platform_os, platform_version, platform_arch, developer_tools)
);
CREATE INDEX evaluation_port ON evaluation (port_id);

-- Every option and computed field the evaluator read, each code checksum
-- as code:<name>, and each option it couldn't read as error:<name>.
CREATE TABLE field (
    evaluation_id INTEGER NOT NULL REFERENCES evaluation (id),
    name TEXT NOT NULL,
    value TEXT
);
CREATE INDEX field_name ON field (name);
CREATE INDEX field_evaluation ON field (evaluation_id);

-- MacPorts' fetch plan, mirror groups expanded.
CREATE TABLE distfile (
    evaluation_id INTEGER NOT NULL REFERENCES evaluation (id),
    ord INTEGER NOT NULL,
    name TEXT NOT NULL,
    url TEXT NOT NULL
);

CREATE TABLE portgroup (
    evaluation_id INTEGER NOT NULL REFERENCES evaluation (id),
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    path TEXT,
    blob TEXT
);
CREATE INDEX portgroup_version ON portgroup (name, version);

-- Paths are kept once; read is the record's shape.
CREATE TABLE path (
    id INTEGER PRIMARY KEY,
    path TEXT NOT NULL UNIQUE
);
CREATE TABLE read_count (
    evaluation_id INTEGER NOT NULL REFERENCES evaluation (id),
    command TEXT NOT NULL,
    subcommand TEXT NOT NULL,
    kind TEXT NOT NULL,
    source TEXT NOT NULL,
    path_id INTEGER NOT NULL REFERENCES path (id),
    count INTEGER NOT NULL
);
CREATE INDEX read_path ON read_count (path_id);
CREATE INDEX read_kind ON read_count (kind, source);
CREATE INDEX read_evaluation ON read_count (evaluation_id);
CREATE VIEW read AS
    SELECT r.evaluation_id, r.command, r.subcommand, r.kind, r.source, p.path, r.count
    FROM read_count r JOIN path p ON p.id = r.path_id;

CREATE TABLE exec (
    evaluation_id INTEGER NOT NULL REFERENCES evaluation (id),
    command TEXT NOT NULL,
    source TEXT NOT NULL,
    count INTEGER NOT NULL
);
CREATE INDEX exec_command ON exec (command);

-- Every call the dispatcher passed, counted by command, file subcommand,
-- who answered, and the port or program a question of the installation
-- was about.
CREATE TABLE ledger (
    evaluation_id INTEGER NOT NULL REFERENCES evaluation (id),
    command TEXT NOT NULL,
    subcommand TEXT NOT NULL,
    source TEXT NOT NULL,
    subject TEXT NOT NULL,
    count INTEGER NOT NULL
);
CREATE INDEX ledger_subject ON ledger (subject);
CREATE INDEX ledger_command ON ledger (command, subject);
CREATE INDEX ledger_evaluation ON ledger (evaluation_id);
-- The registry questions: the ledger's registry_ and _ aliases.
CREATE VIEW registry AS
    SELECT evaluation_id, command AS alias, subject, source, count
    FROM ledger WHERE command GLOB 'registry_*' OR command GLOB '_*';

CREATE TABLE host_access (
    evaluation_id INTEGER NOT NULL REFERENCES evaluation (id),
    problem TEXT NOT NULL,
    frames TEXT NOT NULL
);

CREATE TABLE hook (
    evaluation_id INTEGER NOT NULL REFERENCES evaluation (id),
    phase TEXT NOT NULL,
    origin TEXT NOT NULL,
    body TEXT NOT NULL
);

CREATE TABLE finding (
    port_id INTEGER NOT NULL REFERENCES port (id),
    run_id INTEGER NOT NULL REFERENCES run (id),
    "check" TEXT NOT NULL,
    status TEXT NOT NULL,
    code TEXT NOT NULL,
    detail TEXT
);
CREATE INDEX finding_port ON finding (port_id);

-- What mportinit read, once a run.
CREATE TABLE bootstrap_read (
    run_id INTEGER NOT NULL REFERENCES run (id),
    command TEXT NOT NULL,
    path TEXT NOT NULL,
    value TEXT
);
CREATE TABLE bootstrap_setting (
    run_id INTEGER NOT NULL REFERENCES run (id),
    name TEXT NOT NULL,
    value TEXT,
    origin TEXT NOT NULL
);
CREATE TABLE worker_alias (
    run_id INTEGER NOT NULL REFERENCES run (id),
    name TEXT NOT NULL,
    target TEXT NOT NULL
);

-- The tree's shared files at the commit, by blob.
CREATE TABLE tree_file (
    "commit" TEXT NOT NULL,
    path TEXT NOT NULL,
    blob TEXT NOT NULL,
    PRIMARY KEY ("commit", path)
);

-- The facts table's toolchain for each modelled context a run used.
CREATE TABLE toolchain (
    run_id INTEGER NOT NULL REFERENCES run (id),
    darwin INTEGER NOT NULL,
    arch TEXT NOT NULL,
    profile TEXT NOT NULL,
    xcode TEXT,
    tools TEXT,
    sdks TEXT,
    developer_dir TEXT,
    UNIQUE (run_id, darwin, arch, profile)
);
