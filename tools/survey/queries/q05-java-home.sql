-- Q5: the ports that ask java_home, and with what.
SELECT p.name, e.platform_version, e.platform_arch, x.command, x.source, x.count
FROM exec x
JOIN evaluation e ON e.id = x.evaluation_id
JOIN run ON run.id = e.run_id AND run.mode = 'current'
JOIN port p ON p.id = e.port_id
WHERE x.command GLOB '*java_home*'
ORDER BY p.name, e.platform_version, e.platform_arch;
