-- Q15: licenses with braced groups, the alternatives MacPorts writes {a b}.
SELECT p.name, f.value
FROM field f JOIN evaluation e ON e.id = f.evaluation_id AND e.modeled = 0
JOIN run ON run.id = e.run_id AND run.mode = 'current' JOIN port p ON p.id = e.port_id
WHERE f.name = 'license' AND f.value GLOB '*{*'
ORDER BY p.name;
