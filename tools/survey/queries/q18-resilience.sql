-- Q18: resilience over time: an older snapshot's record attached as older
-- (ATTACH 'older.db' AS older). The ports whose own directory is the same
-- tree in both snapshots and whose fields differ in the Mac's own context:
-- what moved them is _resources or Base, never the port. The joins are
-- written in the order to take them, CROSS JOIN keeping it.
WITH same AS (
    SELECT p.name, p.id AS here, op.id AS there, r.id AS run, orun.id AS older_run
    FROM main.run r CROSS JOIN older.run orun CROSS JOIN main.port p
    CROSS JOIN main.tree_file t CROSS JOIN older.tree_file ot CROSS JOIN older.port op
    WHERE r.mode = 'current' AND orun.mode = 'current'
      AND t."commit" = r."commit" AND t.path = substr(p.portfile, 1, length(p.portfile) - length('/Portfile'))
      AND ot."commit" = orun."commit" AND ot.path = t.path AND ot.blob = t.blob
      AND op.name = p.name
)
SELECT same.name, f.name AS field, o.value AS before, f.value AS after
FROM same
CROSS JOIN main.evaluation e CROSS JOIN older.evaluation oe CROSS JOIN main.field f CROSS JOIN older.field o
WHERE e.port_id = same.here AND e.run_id = same.run AND e.modeled = 0 AND e.outcome = 'observed'
  AND oe.port_id = same.there AND oe.run_id = same.older_run AND oe.modeled = 0 AND oe.outcome = 'observed'
  AND f.evaluation_id = e.id AND o.evaluation_id = oe.id AND o.name = f.name
  AND f.value IS NOT o.value
  AND f.name NOT IN ('dockhand.base_version', 'code:unstable:source_date_epoch')
ORDER BY same.name, field;
