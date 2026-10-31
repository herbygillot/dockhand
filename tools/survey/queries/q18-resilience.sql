-- Q18: resilience over time: an older snapshot's record attached as older
-- (ATTACH 'older.db' AS older). Ports whose fields differ where their own
-- directory didn't change: what moved them is _resources or Base. The
-- portdir blob is the Portfile's code checksum here; compare it first.
SELECT p.name, f.name AS field, o.value AS before, f.value AS after
FROM main.field f
JOIN main.evaluation e ON e.id = f.evaluation_id AND e.modeled = 0 JOIN main.run r ON r.id = e.run_id AND r.mode = 'current'
JOIN main.port p ON p.id = e.port_id
JOIN older.port op ON op.name = p.name
JOIN older.evaluation oe ON oe.port_id = op.id AND oe.modeled = 0
JOIN older.run orun ON orun.id = oe.run_id AND orun.mode = 'current'
JOIN older.field o ON o.evaluation_id = oe.id AND o.name = f.name
WHERE f.value IS NOT o.value
  AND (SELECT value FROM main.field WHERE evaluation_id = e.id AND name = 'code:procedures')
    = (SELECT value FROM older.field WHERE evaluation_id = oe.id AND name = 'code:procedures')
  AND f.name NOT IN ('dockhand.base_version')
ORDER BY p.name, field;
