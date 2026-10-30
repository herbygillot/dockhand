-- Q16: ports whose fetch differs between contexts: a distfile, its
-- location, or a fetch field one context has and another doesn't. Q7's
-- comparison covers what the registry changes.
WITH plan AS (
    SELECT e.port_id, e.id AS evaluation_id, e.platform_version || '/' || e.platform_arch AS context,
           (SELECT group_concat(name || ' ' || url, char(10)) FROM (SELECT name, url FROM distfile d WHERE d.evaluation_id = e.id ORDER BY ord, url)) AS fetch
    FROM evaluation e JOIN run ON run.id = e.run_id AND run.mode = 'current'
    WHERE e.outcome = 'observed'
)
SELECT p.name, count(DISTINCT coalesce(fetch, '')) AS plans, group_concat(context, ' ') AS contexts
FROM plan JOIN port p ON p.id = plan.port_id
GROUP BY p.name HAVING plans > 1
ORDER BY plans DESC, p.name;
