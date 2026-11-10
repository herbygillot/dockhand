# `dockhand backup`: a consistent copy of the database

A feature for v0.9.0, [#92](https://github.com/herbygillot/dockhand/issues/92), placed at the person's word ("Place it for 0.9.0", 2026-11-05 22:00Z, in the architecture thread). The issue and the roadmap's entry say where it stands; this file is the design, architecture's of 2026-11-05.

Herby's ask (project chat, 2026-11-05 21:12Z and 21:14Z): a `dockhand
backup <file>` that backs up the database the correct way, weighed against
cluttering the command surface; the database holds what neither Git nor
GitHub has, check status and history among it. Read at main 543ad194.

## What the database holds, and what sits beside it

- `~/.dockhand/dockhand.db` (or what `--db` / `DOCKHAND_DB` names), 24
  tables at schema 58. Git has the branches, commits, and checkpoint refs;
  GitHub has the pull requests. The database alone has every check's run,
  executions, results, inputs, and evidence, the acceptances, decisions,
  assessments, reviews, change records, network holds, CI failures, the
  branch records with their state, and the journal. No credential is in
  it: the login is in the Keychain or the environment.
- Beside it: `archives/<checkout>/` (kept build archives by digest, often
  gigabytes, rebuilt by a check that can't find them), `logs/` (check
  logs, which cleanup compresses and removes, D6), `serve/` (stamps),
  `config.toml`, `tart/`, `ssh/`.
- What a copy must be: consistent while serve writes. The database is in
  WAL mode, so `cp dockhand.db` alone misses the `-wal`, and copying the
  three files mid-write is uncertain. An APFS snapshot (Time Machine) is
  crash-consistent, so Time Machine covers the disk already, as long as
  the three files are put back together.

## What exists today

- Before a migration, `keepBeforeMigrating` copies the database as it was
  to `dockhand.db.schema-<N>` with `VACUUM INTO`, into a `.partial-*`
  file synced and renamed into place (`keepCopy`), and prunes copies older
  than 30 days at the next migration. It covers one loss: an older build
  that can't open the migrated database.
- The one-liner `sqlite3 ~/.dockhand/dockhand.db ".backup <file>"`, which
  the corrupt-database refusal, the v0.6.0 and v0.7.0 upgrade notes,
  development.md, and acceptance row A5 name.
- Every command that opens the database migrates it first, so a copy made
  after a newer build's `status` is already migrated.

## Three shapes weighed

1. **The one-liner, documented.** Exists; costs nothing. But it leaves to
   the person everything "the correct way" means: which file (`--db`,
   `DOCKHAND_DB`, the default), that `.backup` copies free pages too, that
   archives and logs aren't in it and what a check does without them, and
   whether the file is dockhand's at all. Kept as words, not proposed as
   the answer.
2. **Automatic rotating copies** beside the database, before cleanup's
   daily pass, the last N kept. No surface, and it covers corruption
   (D-T5's case). But it doesn't cover the disk, which Time Machine does;
   it costs the database's size times N beside it, daily; and beside the
   database isn't where a person keeps backups. Not proposed now. If
   unattended copies are wanted later, a `[backup]` section (directory,
   keep) driving serve's daily pass is a second small feature, and it
   would call what this design adds.
3. **`dockhand backup <file>`.** Proposed, below.

## The proposal

**Surface.** A top-level verb in the Occasional group, after `clean`:
undo, archive, clean, explain, decide, config, backup. Dockhand's surface
is flat verbs grouped in `--help`; only `setup` has subcommands (`setup
tart`, `setup github`). A subcommand needs a parent it belongs to, and
none fits: `config` shows settings, `clean` removes, `setup` prepares a
checkout. The cost of the verb is one line under "Occasional:", which is
what answers the clutter concern; nesting wouldn't.

**What it does.** Copies the database `--db`, `DOCKHAND_DB`, or the
default names to `<file>`, whole or not at all: `VACUUM INTO` a
`<file>.partial-*` beside it, synced, renamed into place, `keepCopy`'s way
lifted into one function the migration and the command share. A reader
doesn't block writers in WAL mode, so the copy is a consistent snapshot
while serve runs; it is compacted, a plain SQLite file with no `-wal`.
`<file>` is refused where it exists (a backup never overwrites; name
another); where it names an existing directory the copy is
`<dir>/dockhand-<YYYY-MM-DD-HHMM>.db`; a missing parent directory is made,
as the database's own is. The copy is mode 0600, like the database.

**It doesn't open the engine.** No migration, no session row, no ports
tree, no `--tree`. It opens the file alone, reads `application_id`
(refusing v2's and anything not dockhand's, by name, as `Open` does),
`user_version`, and the newest session's build and time, then copies. So
the v0.9.0 binary backs up the v0.8.0 database before its first migration,
and an older build backs up a database it can't otherwise open.

**Words.** One line on what was copied, one on what the copy lacks:

    Copied ~/.dockhand/dockhand.db, schema 58, last opened by dockhand
    v0.8.0 at 2026-11-05 14:02, to ~/Backups/dockhand-2026-11-05-2114.db
    (96 MB).
    Kept archives (1.4 GB in ~/.dockhand/archives) and check logs aren't
    in the copy: a check run from it builds again what it can't find, and
    says once which archives it dropped.

`--json` carries source, copy, schema, build, opened_at, and bytes.
Refusals: not a dockhand database, the file exists, nowhere to write, a
copy cut short ("nothing was copied", the partial removed).

**Not in it, on purpose.** Archives and logs (rebuildable or expendable,
gigabytes, and a directory can't be snapshotted by `VACUUM INTO`); the
configuration file; the login. A whole copy is the directory: `cp -c -R
~/.dockhand <dir>` on APFS with serve stopped, which the docs name beside
the command. No `--keep`: a backup command that deletes files is the one
thing it mustn't do; a launchd job prunes by name.

**Restore stays by hand**, documented, because it replaces the live
database: stop serve (quit it; the launchd agent is `dockhand serve
--uninstall`, `--install` after), move `dockhand.db`, `dockhand.db-wal`,
and `dockhand.db-shm` aside together (a stale `-wal` left beside a put-back
copy would be replayed onto it), copy the backup to
`~/.dockhand/dockhand.db`. The next command opens it, migrating it forward
where the build is newer and keeping a schema copy as always; archives
the restored database doesn't name go at the next cleanup, and the next
check says once which it dropped. No `restore` verb: `undo` is tidy's
restore (D5's "restore" is the branch's), and a command replacing the live
database needs guards of its own (live processes, the WAL) for a step
taken after a corruption refusal, when dockhand may not open at all. It
can come later if the three moves prove error-prone.

## Where it goes

- A feature by the categories, placed in v0.9.0 by Herby (2026-11-05
  22:00Z): about a tenth of a batch (inferred), the store and one command
  package. v0.9.0's upgrade note opens with "run `dockhand backup <file>`
  first", which the new binary does before it migrates: the core release
  that migrates the most is where a backup command is most wanted.
- Code: `sqlite.Backup(ctx, path, into) (Backup, error)` in
  internal/store/sqlite (opens with the same pragmas, checks, copies with
  the function `keepCopy` becomes, closes); `internal/command/backup`
  (about 150 lines: `Places.DatabasePath`, the directory and date naming,
  the words, `--json`). Tests: a store with rows copied and opened with
  the same rows; copied while a writer holds a transaction; a v2 file and
  a text file refused; an existing file refused; a directory target's
  name; the partial removed on a failure; the command's words and JSON.
- Acceptance: a row beside A5 and A7 (the migration rows): `dockhand
  backup` of the stage's database while serve runs, the copy opening with
  the same branch count, and a candidate run on it with `DOCKHAND_DB`
  migrating the copy and leaving the original alone. A5 may make its copy
  with `dockhand backup` in place of `sqlite3 .backup`, implementation's
  call.
- The migration copy keeps its name, place, and pruning; nothing removed.

## Usage docs

- docs/usage/configuration.md, "Paths and limits worth knowing", after
  the archives paragraph: `dockhand backup <file>` makes a consistent
  copy of the database while serve runs, the database alone; what a check
  does without the archives; the directory copy for a whole one; the
  three steps to put a backup back. The migration paragraph's "a copy
  named with `DOCKHAND_DB`" says one `dockhand backup` made.
- docs/usage/recovery.md: a last section, "Put back a database from a
  backup", the restore steps, since the corrupt-database refusal sends
  people to a backup.
- README.md: the command table gains "Back up the database |
  `dockhand backup ~/Backups`" after the `clean` row; `dockhand --help`'s
  Occasional group gains "backup  Copy the database, consistently, to a
  file".
- docs/development.md line 39: "a `.backup` copy for a replay" becomes "a
  `dockhand backup` copy".
- The corrupt-database refusal in internal/store/sqlite names `dockhand
  backup` in place of `sqlite3 ".backup"`; the release's upgrade notes
  open with the backup line. docs/usage/agents.md is unchanged.

## Decided (Herby, 2026-11-05 22:00Z)

- Placed in v0.9.0.
- A directory target names the copy by date: kept, architecture's choice
  at Herby's leave. `dockhand backup ~/Backups` is then the whole command
  for a person or a launchd job, each run leaving a new dated file, and
  the refusal of an existing file never meets a name it made itself,
  since the minute is in the name.
- Automatic copies: Herby is unsure of them, and would rather have
  policies, copies at events, than a schedule dockhand can't keep when it
  isn't running. Weighed separately in architecture's backup-policies
  note, for his review; this command is placed as it is.
