-- Q12: GitHub sources: how each port fetches, and the hooks that fetch
-- submodules.
SELECT p.name,
       max(CASE f.name WHEN 'github.tarball_from' THEN f.value END) AS tarball_from,
       max(CASE f.name WHEN 'fetch.type' THEN f.value END) AS fetch_type,
       max(CASE f.name WHEN 'git.url' THEN f.value END) AS git_url,
       (SELECT count(*) FROM hook h WHERE h.evaluation_id = e.id AND h.body GLOB '*submodule*') AS submodule_hooks
FROM field f
JOIN evaluation e ON e.id = f.evaluation_id AND e.modeled = 0
JOIN run ON run.id = e.run_id AND run.mode = 'current'
JOIN port p ON p.id = e.port_id
WHERE e.id IN (SELECT g.evaluation_id FROM portgroup g WHERE g.name = 'github')
GROUP BY p.name ORDER BY p.name;
