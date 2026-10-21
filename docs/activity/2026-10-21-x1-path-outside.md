# X1: a branch found from outside the ports tree

Part 1's item 6, its first step, at the architecture thread's order: (c),
then the port reader to macports/treequery, then the GitHub runtime and
compose.

`dockhand path <branch>` run outside any checkout failed "not a MacPorts
ports tree", though the branch's worktree was recorded, since the tree is
opened before the database (the exiv2 field notes).

- `Store.Repositories` lists the Git common directories registered,
  oldest first.
- `engine.Checkouts` opens the database without a tree and gives each
  registered clone whose .git is still there.
- A command naming a branch, by `-b`, `-p`, or `--pr`, or `path`,
  `status`, and `archive` by its name, takes the one registered checkout.
  That's where no `--tree` or `MACPORTS_TREE` is given, and the directory
  isn't a ports tree or isn't in a Git checkout at all (`notCheckout` is
  `ErrNotPortsTree` too). With several registered, it refuses, naming
  them. `path` alone still means here.
