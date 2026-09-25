# 2026-09-25: diff and impact

Design v3 §5's "Understand" group gains `diff` and `impact` (§6.7).

## What changed

- **`dockhand diff [<path>...]`** shows the branch's change from the master it starts from, with the files as they are now: commits and uncommitted edits alike. That is what `check` captures and what a pull request would show.
  - A header lists each port directory CI would build, marked changed, revision only, new, or removed. Revision-only is proven from the source, the same way plans prove it. It also names the paths CI builds nothing for.
  - Paths narrow the diff. A path is taken from where you are when you are in the worktree, so `dockhand diff Portfile` works from a port's directory. Otherwise it is taken from the top of the tree.
  - `--stat` lists the changed files instead of the patch.
  - `git.DiffTrees` takes optional paths for this.
- **`dockhand impact [<port>...]`** shows:
  - **Changed ports**, from CI's rule.
  - **Other dependents**: the direct build, library, and runtime dependents from the port index at the branch's base, leaving out ports the branch changes itself.
    - By default it looks for dependents of every existing port the branch changes beyond its revision. A rebuild's dependents aren't in question.
    - Naming ports asks about those instead, changed or not.
    - Dependents are worded as candidates, and it suggests `check --also` for them.
    - When the index can't be read, it says so and shows the rest.
  - **Shared files**: changed `_resources` files. For a PortGroup file, it lists the Portfiles at the base that load that group and version, found with `git grep`.
- **`DependentReader`** is the engine's seam for dependents. The real reader stages the index through the same `portindex.Stager` the evaluator uses and reads `ReverseDependencies`.

## Tests

- **Engine:**
  - `diff` covers a committed substantive change, an uncommitted revision bump, a changed PortGroup, and a file CI doesn't build; narrowing by a path is included.
  - `impact` checks that:
    - the revision-bumped harbor-cli is not asked about, and not listed as another dependent;
    - the PortGroup's user is found;
    - an unchanged port can be named.
- **Command:** `update`, then:
  - `diff`, `diff --stat Portfile` from the port's directory, and a path with no changes;
  - `impact`, with dependents, then after reducing the change to a revision bump.

**Not yet:**
- `diff --archive`, which needs the distfiles fetched.
- The design's "Last 6 libharbor updates on master also rebuilt dependents". Master's history doesn't tie a rebuild to the update that caused it except through PRs, so that line needs a better source than a guess.
