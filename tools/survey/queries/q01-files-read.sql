-- Q1: every file each port reads, in the tree and outside it. Outside is
-- kind host: not the tree, Base's library, or Tcl's. Per path, the ports
-- that read it and how they were answered, in the current run.
SELECT r.kind, r.path, r.source, count(DISTINCT e.port_id) AS ports, sum(r.count) AS calls
FROM read r
JOIN evaluation e ON e.id = r.evaluation_id
JOIN run ON run.id = e.run_id AND run.mode = 'current'
GROUP BY r.kind, r.path, r.source
ORDER BY r.kind = 'tree', ports DESC, r.path;
