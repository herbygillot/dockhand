# 2026-09-21: an evaluation snapshot carries its root

## Why

The [workspace design](../workspace-design.md) evaluates candidates as
overlays, sibling directories of the base projection. Its review found the
comparators unready for that: `fidelity.ComparablePort` replaced one root
string with `<source>` on both sides of every comparison, and option values
such as `filespath` are absolute paths into whichever root evaluated them,
so a baseline from one directory and a candidate from another would have
compared as a changed `filespath` and refused every edit. `Equivalent`
alone took two roots, for the preparation's final check.

## What changed

- `macports.Snapshot` has `Root`, the directory the evaluation ran in, set
  by the evaluator where it builds every snapshot and observation, and
  excluded from JSON. A snapshot read back from a record has none.
- The comparators normalize each side by its own root: `Revision`,
  `Equivalent`, `Checksums`, `GitVersion`, `ScopedVersion`, and
  `ScopedChecksums` lose their root parameters; `ComparablePort` keeps
  its, since the two ad hoc comparisons in `go_toolchain.go` and
  `dependencies.go` pass each snapshot's root, and it leaves values as they
  are for an empty root rather than inserting `<source>` between every
  character. `ScopedVersion`, which copies a member's after state into the
  before snapshot before the final equivalence check, normalizes that
  member by the after root first, so the mixed snapshot compares right
  when the two roots differ.
- Thirteen call sites in `portedit` and the preparation service stop
  passing `input.files.root`.

No behavior changes while every comparison still runs in one directory;
the tests add the two-root cases and the unrooted one.
