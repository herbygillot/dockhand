-- Schema 17: a result keeps why its target stopped, in its provider's
-- words, where the provider says: a Tart guest's MacPorts errors for a
-- failed target, or the changed dependency that blocked one. Schema 11
-- dropped schema 10's detail, which said only what the plan now says (an
-- unmet need); this one is the build's own. Empty for results recorded
-- before it, and where the provider says nothing.
ALTER TABLE results ADD COLUMN detail TEXT NOT NULL DEFAULT '';
