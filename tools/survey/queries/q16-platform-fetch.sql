-- Q16: ports whose fetch differs between contexts: the archives or their
-- locations one context's plan has and another's doesn't. Q7's comparison
-- covers what the registry changes. Each evaluation's plan is digested
-- once, so the whole tree's record answers in seconds.
WITH plan AS (
    SELECT evaluation_id, group_concat(name || ' ' || url, char(10)) AS fetch
    FROM (SELECT evaluation_id, name, url FROM distfile ORDER BY evaluation_id, ord, url)
    GROUP BY evaluation_id
)
SELECT p.name, count(DISTINCT coalesce(plan.fetch, '')) AS plans, count(*) AS contexts,
       group_concat(e.platform_version || '/' || e.platform_arch, ' ') AS observed_in
FROM evaluation e
JOIN run ON run.id = e.run_id AND run.mode = 'current'
JOIN port p ON p.id = e.port_id
LEFT JOIN plan ON plan.evaluation_id = e.id
WHERE e.outcome = 'observed'
GROUP BY p.name HAVING plans > 1
ORDER BY plans DESC, p.name;
