# status lists dockhand's processes

Roadmap part 1, item 7's last step (the exiv2 agent compared pgrep and
versions by hand before each rebuild, as field testing does).

- Schema 33 adds `sessions.schema`: each session records the newest
  schema its build writes (`model.Session.Schema`), 0 for one an earlier
  build recorded. A migration, which a newer build does on opening the
  database as every one before it.
- `Store.OpenSessions` reads the sessions that haven't ended, oldest
  first. `engine.Processes` judges each by coord's liveness, leaves out
  the asking process's own and dead ones, and says which leads the queue
  and which checks each runs; where a process expects a schema other than
  the database's, as the asking session records it, `Expects` and
  `Database` say both.
- status lists them under "Dockhand processes", PID, kind, build, since,
  and what each does, with a "!" line for a schema mismatch; `--json`
  gives `processes`. Commands that open no session, such as submit and
  update, stay unlisted.

With this, item 7 is done.
