-- Schema 6: a baseline run builds a branch's failed ports at its base, and
-- names the run whose failures it looks into (Design v3 §6.8). It is
-- evidence about that run, never the branch's own check.
ALTER TABLE runs ADD COLUMN baseline_of TEXT NOT NULL DEFAULT '';
