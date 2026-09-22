# 2026-09-22: the smaller items, triaged and mostly done

The roadmap carried twelve small items found during the day. Each was
checked against the code at `6b88b1fc` before anything was touched;
three came off as closed or wrong, seven were done in one evening, and
two stay carried with the reason.

## Closed without code

- **The derived baseline copy** landed with the editing session earlier
  in the evening.
- **The flyctl pull request** [#34824](https://github.com/macports/macports-ports/pull/34824)
  was merged on 2026-09-21; there is no decision left.
- **The mirror seed "downloads an index it then discards"** was
  overstated: the seed opens a GET, reads only the headers for
  Last-Modified, and runs every bracket check before the body is read at
  the copy, so a failed bracket costs one connection, not an index. A
  HEAD first would save that connection and nothing else.

## Done

- **An update onto a stub's carrier.** The onto selection is the
  contribution's target, the carrier subport, and the stub resolver
  answers nothing for a subport, so the binding recorded no stub and no
  shared release, and a bump onto an adopted py-foo branch edited the
  carrier alone. The binding now looks the carrier up among its owner's
  members and records the stub as a fresh bump of the stub would. Along
  the way: the stub is recorded for every action and the shared-release
  authorization only for a bump, since the action rule refuses it
  elsewhere, which had made a revision bump of a stub refusable.
- **`--dry-run --adopt` created the database.** `BuildForReading`
  assembles the services around a store opened for reading when the
  database exists, and around none otherwise; a store-less adoption in a
  dry run skips the records. The preview's adopt path and `adopt
  --dry-run` use it, and the CLI tests assert that no database appears.
- **Category selection of an uncategorized port.** A port indexed with
  no categories, an obsolete port or an R port, is placed by its
  directory's category rather than reported as of unknown membership, so
  `--category` and `--all` agree; a maintainer selection of the same port
  is still answered from the index alone.
- **The pull request checklist's hand walk.** The evidence projection
  carries the recorded steps and answers which step passed a phase for a
  package; the body reads that, and no evidence walk is left outside
  `workflow/view`.
- **Run roots left by test binaries.** The five packages whose tests
  reach `scratch` without a `TestMain`, git, dependency, patchcheck,
  portedit, and workspace, have one now, keeping their run roots out of
  the user's temporary directory. The per-test `TMPDIR` overrides in
  `cli` were not redundant as the item said: each isolates an assertion
  about what the command left behind, and they stay.

## Carried

- **The reader's own `staged` map** in `app/selection.go`, keyed by root
  and tree where the registry keys workspaces by source. Moving the mark
  onto the workspace widens a `macports` interface with a `portindex`
  concept for one caller; it waits for a second staging site.
- **The category enumeration in `eval.Resolve`** is reachable only
  through the bare evaluator, which setup and tests use; every command
  reads through the index-backed reader. A note, not work.
- **`workspace.Adopt`** stays: its callers are tests in two packages,
  which is what an exported constructor over unexported fields is for,
  and its comment says so.
- **The survey's lost cores** are a measurement, a mutex and CPU profile
  over one category survey, before they are anything else.
