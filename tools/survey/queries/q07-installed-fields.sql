-- Q7: do the registry's answers reach the fields an update edits? Each
-- port that asked, in each context that asked, whose fields differ
-- between the fresh installation (current) and the installed one.
WITH pairs AS (
    SELECT DISTINCT c.id AS fresh, i.id AS installed, c.port_id, c.platform_version, c.platform_arch
    FROM registry g
    JOIN evaluation c ON c.id = g.evaluation_id JOIN run cr ON cr.id = c.run_id AND cr.mode = 'current'
    JOIN evaluation i ON i.port_id = c.port_id AND i.platform_os = c.platform_os AND i.platform_version = c.platform_version
        AND i.platform_arch = c.platform_arch AND i.developer_tools = c.developer_tools
    JOIN run ir ON ir.id = i.run_id AND ir.mode = 'installed'
)
SELECT p.name, pairs.platform_version, pairs.platform_arch, cf.name AS field, cf.value AS fresh, inf.value AS installed
FROM pairs
JOIN port p ON p.id = pairs.port_id
JOIN field cf ON cf.evaluation_id = pairs.fresh
LEFT JOIN field inf ON inf.evaluation_id = pairs.installed AND inf.name = cf.name
WHERE cf.name IN ('version', 'revision', 'distfiles', 'master_sites', 'checksums', 'go.vendors', 'cargo.crates')
  AND cf.value IS NOT inf.value
ORDER BY p.name, field;
