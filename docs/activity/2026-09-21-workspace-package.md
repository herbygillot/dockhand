# 2026-09-21: the workspace package

## What landed

Step 3 of the [workspace design](../workspace-design.md), the package
itself, before any consumer adopts it:

- `git.MaterializeInto` writes a tree's blobs under an existing directory
  for the pathspecs given, or all of them, skipping paths already present;
  `Materialize` is now a call to it. `git ls-tree` does not take the
  exclude pathspec magic, so the skip happens in Go after the listing.
- `macports/workspace`: `Open` claims a directory and materializes nothing;
  `EnsurePort` brings a port's directory and `_resources`, `EnsureAll` the
  rest; `Scope` says what is there; `Overlay` makes a sibling projection
  from the base's tree entries, hardlinking unchanged files, re-creating
  symlinks from their entries so a symlink never becomes its target, writing
  edited files with their entries' modes, and leaving untracked files such
  as a staged index out; `Commit` writes the overlay's edits over the base
  tree; `Batch` opens one interpreter session per base and hands it to the
  overlays; `Close` closes the session, then removes the directory. An
  overlay's scope is its base's, and ensuring through an overlay widens the
  base and links what it gained.
- `macports.Tree` carries a base root, `NewTreeOver` sets one, and the
  evaluator's session guard compares bases, so an overlay evaluates in the
  session bound to its base. The live test evaluates a base, then an
  overlay with a changed version in the same session, and sees the new
  version with `filespath` naming the overlay; a second base is refused as
  before.
- `workspace.ScopeOf(root)` answers for open workspaces by root, and two
  consumers that walk the root refuse a sparse one: index generation in
  `portindex`, since a port absent from the projection would be indexed as
  absent from the tree and cached under the tree id, and bare-name
  resolution's category enumeration in `eval.Resolve`. A root that is no
  workspace's, a plain materialization, is taken as whole.

## What has not landed

No consumer uses the package yet; `git.Snapshot`, `portedit.workspace`,
and `withContents` are as they were. The next change adopts it in the
preparation service and `portedit`, where the materializations and the
in-place candidate write live, and measures a real bump before and after.
