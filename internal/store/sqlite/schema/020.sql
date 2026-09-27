-- Schema 20: an execution can stand for earlier builds rather than build.
-- A check whose every target in an environment would read what an earlier
-- build of it read, recorded as its inputs, reuses that build's result
-- (decision 28). The execution is marked reused, and runs nothing; each of
-- its results names the execution that built it. Both are empty for what
-- was built.
ALTER TABLE executions ADD COLUMN reused INTEGER NOT NULL DEFAULT 0;
ALTER TABLE results ADD COLUMN reused_from TEXT NOT NULL DEFAULT '';
