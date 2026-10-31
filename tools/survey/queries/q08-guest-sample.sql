-- Q8, the sample: ports with a host-in-model, host, process, or
-- enumeration ledger entry in a modelled context, one per PortGroup, and
-- 200 more at random. Write the names to a file for -sample.
WITH flagged AS (
    SELECT DISTINCT e.port_id
    FROM ledger l JOIN evaluation e ON e.id = l.evaluation_id JOIN run ON run.id = e.run_id AND run.mode = 'current'
    WHERE e.modeled = 1 AND l.source IN ('host-in-model', 'host', 'process', 'enumeration')
),
per_group AS (
    SELECT min(e.port_id) AS port_id FROM portgroup g JOIN evaluation e ON e.id = g.evaluation_id GROUP BY g.name
),
random AS (
    SELECT id AS port_id FROM port WHERE id NOT IN (SELECT port_id FROM flagged UNION SELECT port_id FROM per_group) ORDER BY random() LIMIT 200
)
SELECT name FROM port WHERE id IN (SELECT port_id FROM flagged UNION SELECT port_id FROM per_group UNION SELECT port_id FROM random) ORDER BY name;
