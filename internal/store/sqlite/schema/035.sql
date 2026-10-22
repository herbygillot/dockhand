-- Schema 35: the decision record (v0.5.0 part 2, item 2). A person's
-- answer to a finding, which a later command reads in place of asking
-- again: the finding it answers, by its kind and subject, the evidence it
-- rests on, and the answer. An acceptance of a port's failure at one
-- commit was the first kind, and its rows move here unchanged in meaning.
-- branch_id is null for a standing decision, which answers its finding on
-- any branch while its evidence holds.

CREATE TABLE decisions (
 repository_id TEXT NOT NULL,
 branch_id TEXT,
 finding TEXT NOT NULL,
 subject TEXT NOT NULL,
 evidence TEXT NOT NULL,
 answer TEXT NOT NULL,
 at INTEGER NOT NULL,
 FOREIGN KEY(repository_id, branch_id) REFERENCES branches(repository_id, id)
) STRICT;

CREATE UNIQUE INDEX decision_key ON decisions(repository_id, ifnull(branch_id, ''), finding, subject, evidence);

INSERT INTO decisions(repository_id, branch_id, finding, subject, evidence, answer, at)
 SELECT repository_id, branch_id, 'failure', port, commit_id, 'accepted', at FROM acceptances;

DROP TABLE acceptances;
