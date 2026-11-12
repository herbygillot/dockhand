-- Schema 62: an attempt's launch note (docs/designs/check-lifecycle.md,
-- "Stopping", #193). What a provider is about to start, in a form its Stop
-- reads, saved under the attempt's launch lock before anything starts;
-- empty where nothing was launched, and on an attempt from before.

ALTER TABLE executions ADD COLUMN launch TEXT NOT NULL DEFAULT '';
