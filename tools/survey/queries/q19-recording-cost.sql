-- Q19: what recording costs: each run, its ports, and its seconds a port;
-- a run with recording off over the same ports, in the same hour, is the
-- baseline.
SELECT id, mode, recording, ports, wall_seconds, user_seconds + system_seconds AS cpu_seconds,
       round(wall_seconds / ports, 4) AS wall_per_port, round((user_seconds + system_seconds) / ports, 4) AS cpu_per_port,
       load_at_start, started
FROM run ORDER BY id;
