-- Q8, the comparison: the guest's record attached as guest
-- (ATTACH 'guest.db' AS guest), its host run against this current run,
-- port by port and context by context: the fields that differ.
SELECT p.name, e.platform_version, e.platform_arch, f.name AS field, f.value AS here, gf.value AS guest
FROM main.field f
JOIN main.evaluation e ON e.id = f.evaluation_id JOIN main.run r ON r.id = e.run_id AND r.mode = 'current'
JOIN main.port p ON p.id = e.port_id
JOIN guest.port gp ON gp.name = p.name
JOIN guest.evaluation ge ON ge.port_id = gp.id AND ge.platform_version = e.platform_version AND ge.platform_arch = e.platform_arch AND ge.developer_tools = e.developer_tools
JOIN guest.run gr ON gr.id = ge.run_id AND gr.mode = 'host'
JOIN guest.field gf ON gf.evaluation_id = ge.id AND gf.name = f.name
WHERE f.value IS NOT gf.value AND f.name NOT IN ('dockhand.base_version')
ORDER BY p.name, field;
