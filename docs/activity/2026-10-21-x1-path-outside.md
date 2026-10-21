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

## The port reader leaves engine (CX-5's package half)

At the architecture thread's word: `internal/macports/treequery`, not
`sourcequery`, since "source" at the top level means upstream's.

- `treequery.Reader` is engine's `evaluatedPorts`: a directory's ports,
  its family, a port's directory, the index's spellings, dependents, the
  ports each directory defines, and the dependency graph. With it come
  archive planning and fetching (`FetchArchives`, `ArchivePlan`,
  `FetchPlanned`), and the types its answers are, `Dependent`,
  `FetchedArchive`, `ErrNoArchives`, and `ErrNoPort`, which engine names
  as treequery's and doesn't alias. `NotInTree` says a port a tree
  hasn't, for a reader standing in for this one.
- distfetch's `OwnArchives` went with its one caller, as treequery's
  `ownArchives`, and distfetch no longer imports depblock.
- treequery imports nothing of engine, whose interfaces it satisfies by
  its methods. engine still builds it as its default reader, in
  `portReader`, until compose fills it. engine's boundary test names it.
- Its tests moved with it: dependents in the index, fetching from the
  mirror after a stealth update, the fetch plan's refusals, and an
  unknown name being no port.
