-- Schema 26: a result keeps where each step of its build begins in its log,
-- each step's name and line, in the order they ran, so a port's own build
-- can be found after its dependencies' installs (the hugo exercise: hugo's
-- own phases began near line 46,400 of 47,000). Empty where its provider
-- didn't record them, and for results made before it.
ALTER TABLE results ADD COLUMN steps TEXT NOT NULL DEFAULT '';
