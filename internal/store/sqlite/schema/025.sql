-- Schema 25: what upstream's change means for each port a revision changes,
-- against the revision's base (the assessment design, D). An assessment
-- is the revision's, by its tree and base, not an edit's; recording one
-- again for the same replaces it, as rules or observations it was made
-- from change.

CREATE TABLE assessments (
 repository_id TEXT NOT NULL,
 branch_id TEXT NOT NULL,
 tree TEXT NOT NULL,
 base TEXT NOT NULL,
 port TEXT NOT NULL,
 directory TEXT NOT NULL,
 comparison TEXT NOT NULL CHECK(json_valid(comparison)),
 policy INTEGER NOT NULL CHECK(policy >= 1),
 at INTEGER NOT NULL,
 PRIMARY KEY(repository_id, branch_id, tree, base, port),
 FOREIGN KEY(repository_id, branch_id) REFERENCES branches(repository_id, id)
) STRICT;
CREATE INDEX assessment_branch ON assessments(repository_id, branch_id, at);
