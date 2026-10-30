-- Q7: do the registry's answers reach the fields an update edits? Each
-- port that asked, in each context that asked, whose fields differ
-- between the fresh installation (current) and the installed one.
WITH asked AS (
    SELECT DISTINCT e.port_id, e.platform_os, e.platform_version, e.platform_arch, e.developer_tools
    FROM registry g JOIN evaluation e ON e.id = g.evaluation_id JOIN run ON run.id = e.run_id AND run.mode = 'current'
),
fields AS (
    SELECT run.mode, e.port_id, e.platform_os, e.platform_version, e.platform_arch, e.developer_tools, f.name, f.value
    FROM field f JOIN evaluation e ON e.id = f.evaluation_id JOIN run ON run.id = e.run_id
    WHERE f.name IN ('version', 'revision', 'distfiles', 'master_sites', 'checksums', 'go.vendors', 'cargo.crates')
)
SELECT p.name, a.platform_version, a.platform_arch, c.name AS field, c.value AS fresh, i.value AS installed
FROM asked a
JOIN port p ON p.id = a.port_id
JOIN fields c ON c.mode = 'current' AND c.port_id = a.port_id AND c.platform_version = a.platform_version AND c.platform_arch = a.platform_arch AND c.developer_tools = a.developer_tools
JOIN fields i ON i.mode = 'installed' AND i.port_id = a.port_id AND i.platform_version = a.platform_version AND i.platform_arch = a.platform_arch AND i.developer_tools = a.developer_tools AND i.name = c.name
WHERE c.value IS NOT i.value
ORDER BY p.name, field;
