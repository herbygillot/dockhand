-- Q11: versioned subports, their livechecks as declared and as resolved.
SELECT p.name,
       max(CASE f.name WHEN 'version' THEN f.value END) AS version,
       max(CASE f.name WHEN 'dockhand.livecheck_declared' THEN f.value END) AS declared,
       max(CASE f.name WHEN 'livecheck.type' THEN f.value END) AS type,
       max(CASE f.name WHEN 'livecheck.url' THEN f.value END) AS url
FROM field f
JOIN evaluation e ON e.id = f.evaluation_id AND e.modeled = 0
JOIN run ON run.id = e.run_id AND run.mode = 'current'
JOIN port p ON p.id = e.port_id
WHERE p.name GLOB '*[0-9]*' AND p.portfile NOT GLOB '*/' || p.name || '/Portfile'
GROUP BY p.name ORDER BY p.name;
