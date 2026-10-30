-- Q13: plain-HTTP homepages, master sites, livecheck URLs, and distfile
-- locations.
SELECT p.name, f.name AS what, f.value
FROM field f JOIN evaluation e ON e.id = f.evaluation_id AND e.modeled = 0
JOIN run ON run.id = e.run_id AND run.mode = 'current' JOIN port p ON p.id = e.port_id
WHERE f.name IN ('homepage', 'master_sites', 'livecheck.url') AND f.value GLOB '*http://*'
UNION ALL
SELECT DISTINCT p.name, 'distfile', d.url
FROM distfile d JOIN evaluation e ON e.id = d.evaluation_id AND e.modeled = 0
JOIN run ON run.id = e.run_id AND run.mode = 'current' JOIN port p ON p.id = e.port_id
WHERE d.url GLOB 'http://*'
ORDER BY 1, 2;
