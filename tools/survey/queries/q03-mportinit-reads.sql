-- Q3: what mportinit reads, once a run.
SELECT run.mode, b.command, b.path, b.value
FROM bootstrap_read b JOIN run ON run.id = b.run_id
ORDER BY run.id, b.command, b.path;
