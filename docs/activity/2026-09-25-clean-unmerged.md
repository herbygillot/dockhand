# 2026-09-25: clean --closed and --archived, and worktrees that come back

The [automatic cleanup note](2026-09-25-automatic-cleanup.md) left this open: a closed or archived branch's work isn't merged, so only its worktree could go, and nothing could check a removed worktree out again.

## What changed

- **A managed branch's worktree comes back when it's needed.**
  - `dockhand path`, and every command that works in the worktree (`update`, `checksums`, `create`, `edit`, `check`, `tidy`, and the others), check a branch out again when its worktree is gone. The new worktree is sparse, as `start` makes one: `_resources` and the ports the branch changes from its base.
  - Git's record of a worktree whose directory went missing is pruned first.
  - A journal event says where it was checked out.
  - A branch dockhand didn't create, or whose Git branch is gone too, is still an error, and says which.
- **`clean --closed` and `clean --archived`** take the worktrees of branches whose pull request closed unmerged, or that were archived. Each worktree is kept if it has edits or untracked files, as with merged branches.
  - The Git branch, your fork's branch, and dockhand's checkpoints and snapshots all stay, since the work isn't merged.
  - The preview says so: "keep branch dockhand/x: dockhand path x checks it out again".
  - The flags combine. `--merged` is still the default, and still the only kind automatic cleanup takes.
- **`git.PruneWorktrees`** forgets worktrees whose directories are gone.

## Decisions

- **Not in automatic cleanup.** Removing an unmerged branch's worktree is recoverable now, but it's still a person's call when they're done with it. Decision 36's schedule stays with merged branches, where the work is safe upstream.
- **Checkpoints stay for unmerged work.** They are what `restore` needs. Only a merged branch whose everything was removed drops them, as before.
