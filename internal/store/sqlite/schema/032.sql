-- Schema 32: the ports a branch was started for (the command-line UX
-- second look's §1). start --port jq made a branch that check -p jq
-- couldn't find, "no tracked branch changes jq", since a branch's ports
-- came only from what it changes. A branch keeps the ports it was started
-- for, by name, separated by spaces, beside what it changes; empty for
-- none, as every branch before this has.

ALTER TABLE branches ADD COLUMN ports TEXT NOT NULL DEFAULT '';
