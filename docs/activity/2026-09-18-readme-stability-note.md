# The README's stability note

Dockhand has been submitted to MacPorts, by dockhand. The README's warning, which said commands, behavior, and stored data could all change or break without notice, no longer described the project: the state database has been migrated forward since schema 2 and is never discarded, and `db backup` keeps a copy before a migration. The warning is now a note that says so, and keeps the honest part: commands, flags, output, and the workflow may still change between releases, so scripts built on them should expect to be revisited.
