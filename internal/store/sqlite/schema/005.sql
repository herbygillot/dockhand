-- Schema 5: the reviews dockhand made of pull requests, so the next review
-- of the same one can say which findings are resolved (Design v3 §6.11).
CREATE TABLE reviews (
 repository_id TEXT NOT NULL,
 pr_repository TEXT NOT NULL,
 number INTEGER NOT NULL CHECK(number > 0),
 head TEXT NOT NULL,
 findings TEXT NOT NULL CHECK(json_valid(findings)),
 posted TEXT NOT NULL CHECK(posted IN ('','comment','request-changes')),
 at INTEGER NOT NULL,
 FOREIGN KEY(repository_id) REFERENCES repositories(id)
) STRICT;
CREATE INDEX review_pr ON reviews(repository_id, pr_repository, number, at);
