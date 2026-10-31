-- Q17: PortGroup reliance: each PortGroup version, its file, how many
-- ports load it, and how many of their code checksums it may move. Join
-- the assumption manifest on name.
SELECT g.name, g.version, g.blob, count(DISTINCT e.port_id) AS ports,
       (SELECT count(*) FROM field f JOIN evaluation e2 ON e2.id = f.evaluation_id
        WHERE f.name GLOB 'code:*' AND e2.id IN (SELECT evaluation_id FROM portgroup WHERE name = g.name AND version = g.version)) AS code_fields
FROM portgroup g
JOIN evaluation e ON e.id = g.evaluation_id JOIN run ON run.id = e.run_id AND run.mode = 'current'
GROUP BY g.name, g.version, g.blob
ORDER BY ports DESC;
