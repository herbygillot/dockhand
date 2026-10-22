-- Schema 38: what held a submission plan that took the network to find,
-- another open pull request for its ports (or not knowing), and a
-- Git-fetched source whose tag moved, kept by branch and the tree the
-- plan was of, so status, which never reads the network, says them as
-- the last plan found them (C2's rest, v0.5.0 part 4, item 1). A plan of
-- the same tree replaces them whole; another tree's are its own.

CREATE TABLE network_holds (
 repository_id TEXT NOT NULL,
 branch_id TEXT NOT NULL,
 tree TEXT NOT NULL,
 origin TEXT NOT NULL,
 port TEXT NOT NULL,
 rule TEXT NOT NULL,
 path TEXT NOT NULL,
 subject TEXT NOT NULL,
 source TEXT NOT NULL,
 class TEXT NOT NULL,
 detail TEXT NOT NULL,
 at INTEGER NOT NULL,
 FOREIGN KEY(repository_id, branch_id) REFERENCES branches(repository_id, id)
) STRICT;

CREATE INDEX network_hold_tree ON network_holds(repository_id, branch_id, tree);
