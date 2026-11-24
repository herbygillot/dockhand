-- Schema 66: the change request keeps every ask (#195). No table changes:
-- the request's new fields, each ask told given from left out, the
-- outcomes' JudgedBy and the publication's kept request ID, are in the
-- JSON columns schema 65 added. The number still rises, so a dockhand
-- built before it, which would drop those fields when it saved a branch,
-- refuses the database. It waits for quiet, as 65 did.
SELECT 1;
