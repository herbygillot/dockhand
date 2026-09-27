# 2026-09-27: branch worktrees in ~/Source/macports-branches

The person's decision: dockhand puts branch worktrees in `~/Source/macports-branches` by default, wherever the clone is. The default had been a `macports-branches` directory beside the clone. `worktrees` in the configuration, or `init --worktrees`, still names another.

**Why a fixed place.** A person finds their branches in one known directory, and it doesn't move with the clone. `~/Source/macports-branches` is also where the old rule put them for a clone in `~/Source`, as this Mac's is.

**What else could be named.** `~/macports-workspace` was weighed. "Workspace" already names something inside dockhand, the files a revision gives MacPorts to read (`macports/workspace`), and the directory holds one worktree per branch, "branch" being the word dockhand shows.

**What a fixed place costs.** Every clone and database now shares the one directory, and a worktree is named by its branch's short name, so two clones starting `jq-update` would collide. Git refuses the second worktree rather than lose anything. A second clone for real work should set its own `worktrees`.

For the same reason, nothing but a person's own dockhand may use the default:
- the engine's tests name their fixture's worktrees, and so do the script provider's and the live Tart test's, since none of them isolates `HOME`;
- the scratch environment's configuration names its own directory.

A full test run left `~/Source/macports-branches` as empty as it began.

**Moving existing worktrees.** There was nothing to move:
- this Mac's real database was empty;
- `~/Source/macports-branches` held nothing;
- the checkout's worktrees are other tools', not dockhand's.

The only v3 branches with worktrees were the scratch environment's, and they stay in its own directory, apart from real work.

**What changed.**
- `Engine.DefaultWorktrees` is `~/Source/macports-branches`, and beside the clone only where there is no home directory.
- `init` asks "Keep branch worktrees in ~/Source/macports-branches?".
- `dockhand config` shows the default.
- The design's examples use the new path.
