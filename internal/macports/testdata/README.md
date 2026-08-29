# MacPorts test fixtures

Real Portfiles and PortGroups, used two ways: as evaluation fixtures by the
`mport` packages, and as the parser's production-dialect corpus, swept by
`tcl/syntax`'s tiling and differential harnesses.

| Directory | Contents | Source |
|---|---|---|
| `portfiles/` | 200 Portfiles, named `<category>__<port>`, selected for variety: the most complex in the tree, feature buckets (subports, variants, procs, computed versions, obsolete stubs, every version-carrier idiom), size extremes, and a random remainder. | `macports/macports-ports` at `origin/master` commit `19fbf38d68e711853bd3bdd0cbe3010e9199ccb8` (2026-08-27) |
| `portgroups/` | All 103 PortGroups in the tree, from the trivial to the computation-heavy (`muniversal`, `qt5`, `crossbinutils`). | same commit |

All files are unmodified copies from the BSD-3-Clause `macports-ports`
tree, existing solely as test input.

Regenerating: the selection scripts live in this repository's history rather
than in-tree; any future refresh should update the commit hashes above.
