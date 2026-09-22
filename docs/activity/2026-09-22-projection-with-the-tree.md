# 2026-09-22: the projection travels with the tree

The fourth part of Next item 1. The workspace package kept a global map
from root paths to open workspaces, and three functions consulted it:
`EnsurePortAt` for a reader that resolved a name and was about to read
the port, `WidenAt` for the category enumeration and the indexer, and
`ScopeOf` for the whole-tree assertion and the app's decision to skip
validating a projected root. A consumer holding a `macports.Tree` could
materialize more of it without the tree saying so, and whether a root was
registered changed what a call did: an unregistered root was a no-op or
was taken as whole. The second architecture review named it an implicit
second mechanism beside the registry, and it was.

`macports.Tree` carries a `Projection` now: `EnsurePort`, `EnsureAll`, and
`Whole`. A workspace's tree carries the workspace; a plain
materialization's tree carries one that needs nothing and is whole. Name
resolution asks the tree's projection for the port the index named,
category enumeration asks it for the whole tree, `portindex.Stage` takes
the tree rather than a root string and asks its projection before
generating, and the assertion that index generation needs the whole tree
reads `Whole`. The app skips validating a projected tree by asking the
tree. The map, its lock, and the three functions are gone; nothing is
registered by root.

The reader contract written two days ago, that a reader materializes what
it resolves, is now the tree's contract: what a reader may ask for is on
the value it was handed.
