-- Q14: livecheck by type, as declared and as resolved.
SELECT d.value AS declared, t.value AS resolved, count(*) AS ports
FROM field t
JOIN evaluation e ON e.id = t.evaluation_id AND e.modeled = 0
JOIN run ON run.id = e.run_id AND run.mode = 'current'
LEFT JOIN field d ON d.evaluation_id = e.id AND d.name = 'dockhand.livecheck_declared'
WHERE t.name = 'livecheck.type'
GROUP BY declared, resolved ORDER BY ports DESC;
