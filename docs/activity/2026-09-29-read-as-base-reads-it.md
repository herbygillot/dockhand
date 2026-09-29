# 2026-09-29: a dependency and a license file, read as they mean

The hugo exercise's gh, usql, hk, and pgdog run ([review](../reviews/2026-09-28-hugo-bump-exercise.md#gh-usql-hk-and-pgdog-chosen-for-untried-paths)) found dockhand refusing a Portfile MacPorts reads, and holding an update for a license whose text hadn't changed.

## A dependency as Base reads it (finding 5)

mise couldn't be evaluated: `macports: invalid dependency "port:bin/cmake:cmake"`. MacPorts Base has four kinds of dependency, each checked on every write of a `depends_*` option by `portdepends::validate_depends_options` (Base `0c70cb739`, `src/port1.0/portdepends.tcl`):
- `lib:`, `bin:`, or `path:`, then what to look for, then the port that provides it: `^(lib|bin|path):([-A-Za-z0-9_/.${}^?+()|\\]+):([-._A-Za-z0-9]+)$`. Each is met by the port being active, or by its file: a `.dylib` named from the field in the library and framework directories, a program on `PATH`, or a path, relative to the prefix unless absolute. A file that's there and that no port owns drops the dependency altogether (`_get_dep_port`).
- `port:`, then the port alone, or after fields Base reserves and ignores: `^(port)(:.+)?:([-._A-Za-z0-9]+)$`. The comment beside it says "port syntax accepts colon-separated junk that we do not understand yet"; commit `c0a7e26c7` (2008, trac #126) added it so that ports could adopt a future syntax while Base was frozen, and Base's test port `dependencies-d` declares `port:-i_want_b:dependencies-a`.

Every reader in Base takes the port as the last field, except `restore::resolve_depspec` (2024), which takes the second for `port:`, and would read mise's as `bin/cmake`. That looks like a Base bug; dockhand follows the rest. mise is the only port in the tree that uses the reserved form, and the fix there is its Portfile's; dockhand should still read what Base reads.

`macports.ParseDependency` now reads a dependency with Base's two patterns, run through Base's own `switch -regex` on every test case to confirm they agree, and names the last field as the port. The one difference is deliberate: `.` and `..`, which Base's pattern admits as a name, are refused, as `ValidName` refuses them everywhere. The evaluator calls it; it had allowed `port:` only with one field after it, and a middle field of any characters, or none, for the others. `outdated mise` now reads mise, which can be updated.

A gap this leaves, recorded with item 6: dockhand's planner orders a branch's targets by every dependency's port, while Base drops a `bin:`, `lib:`, or `path:` dependency its file meets. In a clean guest, `bin:git:git` is met by the Command Line Tools' git.

## A license whose copyright years moved (finding 1)

Every changed license file held the update, so usql's `LICENSE`, "Copyright (c) 2016-2025" becoming "2016-2026", held `bump`, as zlint's "2024" becoming "2026" did. The hold is there because a relicense needs the Portfile's `license` line to follow, which no build catches; a new year doesn't.

Now, where the two versions of a license file have the same lines, and each line that differs is a copyright line, naming "Copyright", "(c)", or "©", that differs only in its years, the change is said with the line as it now reads, and holds nothing: "LICENSE changed only its copyright years: "Copyright (c) 2016-2026 Kenneth Shaw"". Years are one, or a range or list of them, so "2024" becoming "2024-2026" is a year change too. Anything else holds as before: a new holder, a line added or removed, and a year outside a copyright line, such as a GPL's "Version 2, June 1991", which is the license's own text.

The comparison also read usql's generated Go source `text/license.go` as a license file, since its name begins "license". A file with a program's source extension is no longer one; `LICENSE-MIT` and `COPYING.LESSER` still are.

`update usql --plan --new`, at master c95ae6f with this build, now shows:

```
Upstream changes:
  · LICENSE changed only its copyright years: "Copyright (c) 2016-2026 Kenneth Shaw"
  · go.mod: 1 added, 32 moved
  · go.mod requires Go 1.26.1, which go.toolchain_min 1.26.1 already gates on
```

Tests:
- `TestADependencyIsReadAsBaseReadsIt`, with each case Base's validator was run on, and `TestDecodeMetadataPreservesTclValuesAndDependencySyntax`, with mise's form;
- `TestALicenseWhoseCopyrightYearsMovedHoldsNothing`: a range's end, a range become one year, a list grown by a year, and, holding, a holder, an added line, the license's text, and a year outside a copyright line;
- `TestSourceNamedForALicenseIsNoLicense`.

Ten mutations each fail a test.
