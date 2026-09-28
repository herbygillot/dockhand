-- Schema 21: a run's events are read by the run. A command following a
-- check, and wait, show that run's events; they read them through this
-- index rather than the whole journal from its start, which only grew.
CREATE INDEX event_run ON events(repository_id, run_id, sequence);
