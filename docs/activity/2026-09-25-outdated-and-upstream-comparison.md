# 2026-09-25: outdated, update --outdated, and the upstream comparison

Step 11 of the roadmap, the maintainer loop, first half: Design v3 §6.12.

## What changed

- **`dockhand outdated [port...] [--mine] [--all]`** looks up each port's newest release at MacPorts' master as fetched now.
  - The ports are the ones named, or with `--mine` every port whose maintainers line names you.
  - It lists the ports with newer releases, and what dockhand can do about each: `update`, or `already in <branch>`. It counts the rest. `--all` lists every port, with why any couldn't be checked.
  - It reuses v2's `outdated.Service` and the survey, which now take a commit (`survey.OpenAt`, `Service.Commit`) rather than always HEAD, since in v3 the clone may be on any branch.
  - The engine reaches it through a new `OutdatedReader` seam, so the command is tested with a stand-in.
- **`--mine`** reads you from the config's `maintainer` line. `config.File.Maintainers` turns `{@ada example.org:ada} openmaintainer` into the identities the port index matches, `@ada` and `example.org:ada`.
- **`update --outdated [port...] [--mine] [--check] [--yes]`** first shows how it splits the work: one branch per port, since unrelated ports go in separate PRs. It lists what it skips: ports already in an open branch, and ports that couldn't be checked. On a terminal it asks; without one, `--yes` starts.
  - For each port, it starts a branch from fresh master, updates the port with the upstream archives compared, and commits the edit when the tidy plan is unambiguous. With `--check`, it also queues a check of that commit, in `check.on`'s environments.
  - A problem with one port is reported beside the others, and the command exits 3 when any needs a look.
  - This is `Engine.PlanOutdated` and `Engine.PrepareOutdated`, which `serve.for_outdated` will share.
- **The upstream comparison.** A version update now keeps both versions' archives in a scratch directory and compares them, with `archive.Compare`.
  - This takes a new `KeepArchives` option through preparation into portedit. The new archives are already fetched for their checksums. The current version's are fetched from the base Portfile's own distfiles.
  - It reports three kinds of change:
    - license files (`LICENSE`, `COPYING`, `NOTICE`, and the like) that changed, appeared, or went;
    - top-level build files that changed or are new;
    - declared dependencies from `go.mod` (direct requires only), `Cargo.toml`, `package.json`, `requirements.txt`, and `pyproject.toml`.
  - A license change, a build-file change, or a new dependency is marked `!`: it holds the update for a person's look. A dropped dependency or a version move is information only.
  - `update` prints what it found. A port fetched with git has no archives, so it says nothing. The edit record keeps the comparison (schema 7's `edits.upstream`), and `submit --passing` shows it under each branch.
- **Schema 7** also gives each branch its origin, `person` or `serve`. `serve` will submit only branches it started itself (§11), so this has to be recorded.

## Tests

- **`archive`:** real tar.gz and zip archives:
  - a changed license, an added and a dropped Go module, a moved version, a new meson.build;
  - the other manifest kinds, and a license in `docs/` removed;
  - identical archives saying nothing.
- **Store:** an edit's comparison and a serve branch's origin round-trip, and an edit that compared nothing reads back as nil.
- **Engine:**
  - an update with a stand-in preparer that keeps tarballs finds and records the held changes, and removes its scratch directory;
  - a stand-in outdated reader: the report is at freshly fetched master and sorted; the plan skips a port that couldn't be checked; preparing makes a serve-origin branch with one commit and a queued check, and a second plan skips the port as already in that branch.
- **Command:**
  - `--mine` without a maintainer is refused;
  - `outdated --mine` and `--all`;
  - `update --outdated --mine --check` refused without a terminal, then confirmed on one;
  - `outdated` then shows the port as already in its branch, and a second `update --outdated` finds nothing to start.

**Not proven here:** fetching the old archives from a real Portfile, and discovery against real forges. Both need MacPorts, and v2's discovery is already exercised on a Mac.
