-- Q2: could reuse key on the _resources files a port reads? Each port's
-- _resources reads with their blobs at the commit: two ports that read
-- the same blobs read the same shared input. The guest's fetch trace is
-- the other half (mode host, attached as guest).
SELECT p.name, group_concat(DISTINCT substr(r.path, 10) || '@' || coalesce(t.blob, '?')) AS resources
FROM read r
JOIN evaluation e ON e.id = r.evaluation_id
JOIN run ON run.id = e.run_id AND run.mode = 'current'
JOIN port p ON p.id = e.port_id
LEFT JOIN tree_file t ON t."commit" = run."commit" AND t.path = substr(r.path, 10)
WHERE r.kind = 'tree' AND r.path GLOB '<source>/_resources/*'
GROUP BY p.name
ORDER BY p.name;
