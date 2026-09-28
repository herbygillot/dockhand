# 2026-09-28: source comparison says what it couldn't read

The first step of the private-helper review's item ([review](../reviews/2026-09-28-private-helper-ownership.md), finding 3; [reconciliation](2026-09-28-private-helper-review-reconciled.md)).

## What was wrong

An update compares the old and new source archives for what a reviewer would ask about: license files, build files, and declared dependencies. What it finds that a build can't catch holds a submission nobody reviews, `bump`'s and serve's (D4). The comparison lived in `archive`, beside the walking of tar and zip streams. Each manifest reader returned only a map, so a manifest it couldn't read, or read in a form it didn't know, came back empty: no change, and nothing held.

The review's probes, which failed at `dd21ac87`:
- a Cargo dependency given as its own table, `[dependencies.serde]`, wasn't seen, since only a section named for dependencies was read line by line;
- a pyproject list in single quotes wasn't seen, since only double-quoted strings were matched, anywhere in the file;
- an unreadable package.json compared as no change.

## What changed

The comparison is a package of its own, `internal/sourcecompare`: what a project's files mean belongs apart from how an archive is walked. It reads through `archive.Walk`, and `archive` keeps traversal and extraction.

**Real parsers.**
- go.mod is read with Go's own `modfile`, as `macports/dependency` already does.
- Cargo.toml and pyproject.toml are read with the TOML decoder the tree already vendors.
- A Cargo dependency may be a version, or a table with a version, a Git source, a path, or the workspace's. The package's tables are read, and each target's and the workspace's.
- pyproject's PEP 621 `[project]` array is read in either kind of quote, and so is Poetry's table.

**What it couldn't read, it says.** Each reading can report what it couldn't follow, and that becomes a change of kind `unread`, which holds as a change would:
- a manifest it can't parse, in either version: "upstream's package.json couldn't be read in the new version, so its dependencies weren't compared: unexpected end of JSON input";
- one that reads another file, `-r base.txt` in a requirements.txt, or declares its dependencies dynamically in a pyproject. What it could read is still compared;
- a file past the 1 MiB it reads. It used to be cut short without a word.

A manifest that didn't change isn't read, so an unreadable one that stays the same holds nothing.

**Also:**
- A Cargo dependency given as a table now reads as its version, "adds tokio 1", rather than the table's text.
- The archive fixtures tests build, `Tarball` and `Zipball`, are in `testsupport`, for both packages.
- The engine's import list names `sourcecompare` for update, and `archive` for `diff --archive`.

## Tests

The review's three probes are regression tests. `TestCargoDependenciesAreReadAsTOML`, `TestPyprojectDependenciesAreReadAsTOML`, and `TestWhatTheComparisonCouldntReadHolds` cover the tables, targets, workspace, Poetry, dynamic dependencies, includes, unparseable manifests, truncation, and an unchanged manifest. Eleven mutations each fail a test.
