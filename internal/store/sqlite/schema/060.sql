-- Schema 60: the person's answer to whether dockhand may evaluate another
-- person's Portfiles on this Mac, kept for the pull request and the
-- author's commit it was given for (model.EvaluationConsent, #162); and an
-- adopted branch's author's commit, which the answer is for, apart from
-- pr_pushed, which becomes the person's push once they submit to it.

CREATE TABLE evaluation_consents (
 repository_id TEXT NOT NULL,
 pr_repository TEXT NOT NULL,
 number INTEGER NOT NULL CHECK(number > 0),
 head TEXT NOT NULL CHECK(head <> ''),
 at INTEGER NOT NULL,
 FOREIGN KEY(repository_id) REFERENCES repositories(id),
 PRIMARY KEY(repository_id, pr_repository, number, head)
) STRICT;

ALTER TABLE branches ADD COLUMN pr_author_head TEXT NOT NULL DEFAULT '';
UPDATE branches SET pr_author_head = pr_pushed WHERE pr_adopted = 1;
