-- Schema 44: a failed CI job's reading keeps its log's first error line,
-- read as a local log's is (buildlog.FirstFrom), and each port the job
-- built with its result, as MacPorts' workflow marked them
-- (model.CIFailure.Error, Ports, as JSON), so status quotes the error
-- where it said the log named no cause, and says each port's result
-- (Then's item 7). Empty for every job read before it.

ALTER TABLE ci_failures ADD COLUMN error TEXT NOT NULL DEFAULT '';
ALTER TABLE ci_failures ADD COLUMN ports TEXT NOT NULL DEFAULT '' CHECK(ports = '' OR json_valid(ports));
