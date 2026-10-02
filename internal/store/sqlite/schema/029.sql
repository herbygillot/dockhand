-- Schema 29: what a revision changed in each port directory against its
-- base, per subport, as MacPorts evaluates both (the evaluated-change
-- model's step 1). Like an assessment, a change record is the revision's,
-- by its tree and base, not an edit's; recording one again for the same
-- replaces it.

CREATE TABLE change_records (
 repository_id TEXT NOT NULL,
 branch_id TEXT NOT NULL,
 tree TEXT NOT NULL,
 base TEXT NOT NULL,
 directory TEXT NOT NULL,
 record TEXT NOT NULL CHECK(json_valid(record)),
 policy INTEGER NOT NULL CHECK(policy >= 1),
 at INTEGER NOT NULL,
 PRIMARY KEY(repository_id, branch_id, tree, base, directory),
 FOREIGN KEY(repository_id, branch_id) REFERENCES branches(repository_id, id)
) STRICT;

-- An adopted pull request is someone else's: nothing evaluates its
-- Portfile for a change record. One adopted before this schema is
-- recognized by its adoption's event, where the journal still has it.
ALTER TABLE branches ADD COLUMN pr_adopted INTEGER NOT NULL DEFAULT 0 CHECK(pr_adopted IN (0,1));
UPDATE branches SET pr_adopted = 1 WHERE pr_number IS NOT NULL AND EXISTS (
 SELECT 1 FROM events e WHERE e.repository_id = branches.repository_id AND e.branch_id = branches.id AND e.kind = 'branch.adopt' AND e.message LIKE 'adopted #%');
