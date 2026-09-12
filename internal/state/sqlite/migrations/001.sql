CREATE TABLE repositories (
 id TEXT PRIMARY KEY, common_dir TEXT NOT NULL UNIQUE, created_at INTEGER NOT NULL
) STRICT;
CREATE TABLE sources (
 repository_id TEXT NOT NULL REFERENCES repositories(id), id TEXT NOT NULL,
 commit_id TEXT, tree_id TEXT NOT NULL, base_id TEXT,
 PRIMARY KEY(repository_id,id)
) STRICT;
CREATE TABLE changes (
 id TEXT PRIMARY KEY, repository_id TEXT NOT NULL REFERENCES repositories(id), branch TEXT NOT NULL,
 current_revision TEXT, disposition TEXT NOT NULL CHECK(disposition IN ('open','merged','closed','abandoned')),
 targets TEXT NOT NULL CHECK(json_valid(targets)), created_at INTEGER NOT NULL,
 UNIQUE(repository_id,id),
 FOREIGN KEY(repository_id,id,current_revision) REFERENCES revisions(repository_id,change_id,id) DEFERRABLE INITIALLY DEFERRED
) STRICT;
CREATE UNIQUE INDEX active_branch ON changes(repository_id,branch) WHERE disposition='open' AND branch<>'';
CREATE TABLE revisions (
 id TEXT PRIMARY KEY, repository_id TEXT NOT NULL, change_id TEXT NOT NULL, source_id TEXT NOT NULL,
 previous_id TEXT, created_at INTEGER NOT NULL, UNIQUE(repository_id,id), UNIQUE(repository_id,change_id,id),
 FOREIGN KEY(repository_id,change_id) REFERENCES changes(repository_id,id),
 FOREIGN KEY(repository_id,source_id) REFERENCES sources(repository_id,id),
 FOREIGN KEY(repository_id,change_id,previous_id) REFERENCES revisions(repository_id,change_id,id) DEFERRABLE INITIALLY DEFERRED
) STRICT;
CREATE TABLE requests (
 id TEXT PRIMARY KEY, repository_id TEXT NOT NULL REFERENCES repositories(id),
 kind TEXT NOT NULL CHECK(kind IN ('job','cancel')), payload BLOB NOT NULL,
 accepted_at INTEGER NOT NULL, completed_at INTEGER, UNIQUE(repository_id,id)
) STRICT;
CREATE TABLE jobs (
 id TEXT PRIMARY KEY, repository_id TEXT NOT NULL, request_id TEXT NOT NULL UNIQUE,
 change_id TEXT, spec_change_id TEXT, input_revision TEXT, result_revision TEXT, source_id TEXT NOT NULL,
 action TEXT NOT NULL, destination TEXT NOT NULL, verification TEXT NOT NULL,
 options TEXT NOT NULL CHECK(json_valid(options)),
 state TEXT NOT NULL CHECK(state IN ('queued','active','completed','failed','needs-attention','canceled','superseded')),
 accepted_at INTEGER NOT NULL, cancel_at INTEGER, admitted_at INTEGER, finished_at INTEGER, detail TEXT NOT NULL,
 next_action_at INTEGER, UNIQUE(repository_id,id),
 CHECK(input_revision IS NULL OR spec_change_id IS NOT NULL),
 CHECK(result_revision IS NULL OR change_id IS NOT NULL),
 FOREIGN KEY(repository_id,request_id) REFERENCES requests(repository_id,id),
 FOREIGN KEY(repository_id,change_id) REFERENCES changes(repository_id,id),
 FOREIGN KEY(repository_id,spec_change_id,input_revision) REFERENCES revisions(repository_id,change_id,id),
 FOREIGN KEY(repository_id,change_id,result_revision) REFERENCES revisions(repository_id,change_id,id),
 FOREIGN KEY(repository_id,source_id) REFERENCES sources(repository_id,id)
) STRICT;
CREATE TABLE control_jobs (
 repository_id TEXT NOT NULL, request_id TEXT NOT NULL, job_id TEXT NOT NULL, applied_at INTEGER,
 PRIMARY KEY(request_id,job_id),
 FOREIGN KEY(repository_id,request_id) REFERENCES requests(repository_id,id),
 FOREIGN KEY(repository_id,job_id) REFERENCES jobs(repository_id,id)
) STRICT;
CREATE TABLE plans (
 repository_id TEXT NOT NULL, job_id TEXT PRIMARY KEY, revision_id TEXT NOT NULL,
 targets TEXT NOT NULL CHECK(json_valid(targets)),
 FOREIGN KEY(repository_id,job_id) REFERENCES jobs(repository_id,id),
 FOREIGN KEY(repository_id,revision_id) REFERENCES revisions(repository_id,id)
) STRICT;
CREATE TABLE attempts (
 id TEXT PRIMARY KEY, repository_id TEXT NOT NULL, job_id TEXT NOT NULL, target_id TEXT NOT NULL,
 revision_id TEXT NOT NULL, source_id TEXT NOT NULL, build TEXT NOT NULL CHECK(json_valid(build)),
 state TEXT NOT NULL CHECK(state IN ('queued','submitting','running','finished','uncertain','canceled')),
 claim_owner TEXT, claim_generation INTEGER NOT NULL CHECK(claim_generation>=0), claim_until INTEGER,
 retry_at INTEGER, next_action_at INTEGER, cancel_sent_at INTEGER, cancel_observe INTEGER NOT NULL CHECK(cancel_observe IN (0,1)),
 last_error TEXT NOT NULL, created_at INTEGER NOT NULL,
 UNIQUE(repository_id,id), UNIQUE(repository_id,job_id,id),
 CHECK((claim_owner IS NULL)=(claim_until IS NULL)),
 FOREIGN KEY(repository_id,job_id) REFERENCES jobs(repository_id,id),
 FOREIGN KEY(repository_id,revision_id) REFERENCES revisions(repository_id,id),
 FOREIGN KEY(repository_id,source_id) REFERENCES sources(repository_id,id)
) STRICT;
CREATE TABLE submissions (
 id TEXT PRIMARY KEY, repository_id TEXT NOT NULL, attempt_id TEXT NOT NULL, sequence INTEGER NOT NULL CHECK(sequence>0),
 provider TEXT NOT NULL, run_id TEXT, created_at INTEGER NOT NULL, admitted_at INTEGER, closed_at INTEGER,
 UNIQUE(repository_id,id), UNIQUE(repository_id,attempt_id,id), UNIQUE(attempt_id,sequence), UNIQUE(provider,run_id),
 CHECK(closed_at IS NULL OR run_id IS NULL),
 FOREIGN KEY(repository_id,attempt_id) REFERENCES attempts(repository_id,id)
) STRICT;
CREATE UNIQUE INDEX live_submission ON submissions(attempt_id) WHERE closed_at IS NULL;
CREATE TABLE attempt_evidence (
 repository_id TEXT NOT NULL, attempt_id TEXT PRIMARY KEY, evidence TEXT NOT NULL CHECK(json_valid(evidence)),
 FOREIGN KEY(repository_id,attempt_id) REFERENCES attempts(repository_id,id)
) STRICT;
CREATE TABLE resources (
 id TEXT PRIMARY KEY, repository_id TEXT NOT NULL, attempt_id TEXT NOT NULL, submission_id TEXT NOT NULL,
 provider TEXT NOT NULL, handle TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('active','retained','release-requested','released','uncertain')),
 claim_owner TEXT, claim_generation INTEGER NOT NULL CHECK(claim_generation>=0), claim_until INTEGER,
 retry_at INTEGER, next_action_at INTEGER, retain_until INTEGER, released_at INTEGER, last_error TEXT NOT NULL,
 UNIQUE(repository_id,id), UNIQUE(provider,handle), CHECK((claim_owner IS NULL)=(claim_until IS NULL)),
 FOREIGN KEY(repository_id,attempt_id,submission_id) REFERENCES submissions(repository_id,attempt_id,id)
) STRICT;
CREATE INDEX changes_repo ON changes(repository_id,id);
CREATE INDEX revisions_change ON revisions(repository_id,change_id,id);
CREATE INDEX jobs_repo ON jobs(repository_id,id);
CREATE INDEX jobs_due ON jobs(repository_id,next_action_at,id) WHERE next_action_at IS NOT NULL;
CREATE INDEX attempts_job ON attempts(repository_id,job_id,id);
CREATE INDEX attempts_due ON attempts(repository_id,next_action_at,id) WHERE next_action_at IS NOT NULL;
CREATE INDEX resources_attempt ON resources(repository_id,attempt_id,id);
CREATE INDEX resources_due ON resources(repository_id,next_action_at,id) WHERE next_action_at IS NOT NULL;
CREATE INDEX resources_pending ON resources(repository_id,id) WHERE state IN ('retained','uncertain','release-requested');
CREATE INDEX requests_controls ON requests(repository_id,kind,id);
CREATE INDEX requests_pending_controls ON requests(repository_id,id) WHERE kind='cancel' AND completed_at IS NULL;
CREATE INDEX controls_pending_by_request ON control_jobs(repository_id,request_id,job_id) WHERE applied_at IS NULL;
CREATE INDEX controls_pending ON control_jobs(repository_id,job_id,request_id) WHERE applied_at IS NULL;
