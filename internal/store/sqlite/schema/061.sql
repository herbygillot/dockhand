-- Schema 61: the publication record (docs/designs/publication-record.md,
-- #191). A branch's unfinished submission, one JSON value beside its pull
-- request, as pr_observed is; how dockhand knows that pull request, opened
-- or found, empty on a record from before; the last reviewer request's
-- receipt; and serve's count by its rows, which replaces the count of
-- serve.submit events and outlives the branches.

ALTER TABLE branches ADD COLUMN publication TEXT NOT NULL DEFAULT '';
ALTER TABLE branches ADD COLUMN pr_provenance TEXT NOT NULL DEFAULT '';
ALTER TABLE branches ADD COLUMN pr_rerequested TEXT NOT NULL DEFAULT '';

CREATE TABLE serve_openings (
 repository_id TEXT NOT NULL,
 id INTEGER PRIMARY KEY,
 base TEXT NOT NULL CHECK(base <> ''),
 head TEXT NOT NULL CHECK(head <> ''),
 after INTEGER NOT NULL DEFAULT 0 CHECK(after >= 0),
 holding INTEGER NOT NULL DEFAULT 0 CHECK(holding IN (0, 1)),
 tried INTEGER,
 unanswered INTEGER NOT NULL DEFAULT 0 CHECK(unanswered IN (0, 1)),
 number INTEGER NOT NULL DEFAULT 0 CHECK(number >= 0),
 opened INTEGER,
 gone INTEGER,
 FOREIGN KEY(repository_id) REFERENCES repositories(id)
) STRICT;
CREATE INDEX serve_openings_head ON serve_openings(repository_id, base, head);

-- When this schema was taken on, so the count answers that the limit is
-- reached for the rest of that day (its openings were counted by events),
-- and whether serve's first pass after it has read every branch it
-- prepared that is on the fork with no pull request recorded.
CREATE TABLE serve_openings_upgrade (
 repository_id TEXT PRIMARY KEY,
 at INTEGER NOT NULL,
 first_pass_done INTEGER NOT NULL DEFAULT 0 CHECK(first_pass_done IN (0, 1)),
 FOREIGN KEY(repository_id) REFERENCES repositories(id)
) STRICT;
INSERT INTO serve_openings_upgrade(repository_id, at) SELECT id, CAST(unixepoch('subsec') * 1000 AS INTEGER) FROM repositories;

-- An older submit's reviewer left-over, said once and never sent: where a
-- pull request's reviewers requested changes, a push was recorded after
-- that review, and no request was recorded since, the receipt says the
-- reviewers may not have been asked. A left-over whose events were pruned
-- isn't found, and isn't guessed.
UPDATE branches SET pr_rerequested = json_object('Head', pr_pushed, 'Unasked', json('true'))
WHERE pr_number IS NOT NULL AND pr_observed <> '' AND pr_rerequested = ''
 AND json_array_length(json_extract(pr_observed, '$.ChangesRequestedBy')) > 0
 AND json_extract(pr_observed, '$.ReviewedAt') IS NOT NULL
 AND EXISTS (SELECT 1 FROM events WHERE events.repository_id = branches.repository_id AND events.branch_id = branches.id AND events.kind = 'branch.push'
  AND events.at > CAST(unixepoch(json_extract(branches.pr_observed, '$.ReviewedAt'), 'subsec') * 1000 AS INTEGER))
 AND NOT EXISTS (SELECT 1 FROM events WHERE events.repository_id = branches.repository_id AND events.branch_id = branches.id AND events.kind = 'branch.rerequest'
  AND events.at >= CAST(unixepoch(json_extract(branches.pr_observed, '$.ReviewedAt'), 'subsec') * 1000 AS INTEGER));
