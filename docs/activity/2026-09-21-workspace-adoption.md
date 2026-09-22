# 2026-09-21: the preparation path runs on workspaces

## What landed

Step 3 of the [workspace design](../workspace-design.md): the preparation
service and `portedit` read a workspace instead of a whole-tree
materialization, and every candidate is an overlay.

- `preparation.Service.open` opens a workspace; nothing is materialized
  until the editor resolves the target. The committed candidate is checked
  in a second workspace holding only the target's directory and
  `_resources`. `git.Snapshot` is gone from this path.
- `portedit.Request` and `ProbeSource` carry a `*workspace.Workspace` in
  place of a root. `sourceInput` holds the workspace, the tree, and the
  overlays it made; `withContents` and the in-package `workspace` type are
  gone. `projection` returns the workspace for the contents it holds on
  disk and an overlay otherwise; `evaluateContents`, `observeProfiles`, and
  `observeContents` evaluate in the projection. A multi-profile observation
  reads one immutable overlay with eight interpreters instead of writing
  once and reading many.
- Overlays live as long as their input and close with it, because the
  paths an evaluation records, `filespath` first, name the overlay, and the
  patch check and the checksum helpers read them later.
- `assess` and `outdated` adopt the survey's full materialization as a
  workspace and hand it to every probe; `workspace.Adopt` describes a
  directory someone else filled, with entries from a walk, and `Rescan`
  for a test that adds files afterwards.
- Where a consumer needs more than the workspace holds, the workspace is
  widened by root: index generation calls `WidenAt` before the indexer
  lists the root, the evaluator's category enumeration does the same for a
  bare name, and the selection reader ensures a port's directory once the
  index named it. The reader skips the ports-tree check for a workspace,
  since the repository was validated at registration and the directory may
  hold nothing yet. The design's refusal became widening; the assertion
  that nothing generates an index over a sparse root still holds, since
  widening precedes generation.

## What the change found

- Every path a comparison reads must come from the projection that
  produced it. Three sites compared an overlay's evaluation against the
  base's directory: the archive policy check and the distfile binding took
  the base's port directory and Portfile path, and the obsolete-follower
  check compared options across two overlays with `maps.Equal`. Each now
  takes the root its snapshot names, and the follower check normalizes both
  sides with `fidelity.ComparablePort`.
- A derived input that replaces its baseline contents, the stripped form
  the dependency path builds, must not be mistaken for what the workspace
  holds: `projection` compares against the loaded contents, not the input's
  baseline.

## Measured

`dockhand bump jq 1.7.1 --dry-run` on the ports tree, release resolution
and preparation with the candidate check, twice each:

| binary | run 1 | run 2 |
| --- | ---: | ---: |
| before, `0f76d4e7` | 24.2 s | 20.4 s |
| after | 3.5 s | 3.0 s |

Both produce the same diff. The difference is the materializations: three
whole-tree writes and removals of 34,614 files each, replaced by three
projections of one port directory and `_resources`.

## Evidence

The full suite passes; the portedit, preparation, CLI, assess, and outdated
tests run the path end to end under port-tclsh, and the workspace tests
cover the projection rules directly.
