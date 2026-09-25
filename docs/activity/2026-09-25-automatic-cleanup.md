# 2026-09-25: automatic cleanup

Decision 36 for v3: nothing depends on a person remembering to clean up.

## What changed

- **`Engine.Cleanup`** removes two things. It never touches open branches, or work of anyone's own:
  - what `clean --merged` would remove from merged branches, leaving everything clean would keep;
  - port index generations unused for longer than the cleanup age. This uses `portindex.Collect`, which already takes each cache profile's lock, skips a busy one, and keeps the latest generation.
- **`serve` runs it at most once a day** per database, when the queue is empty, so it never delays a check.
  - The last run is the time of a `cleanup.stamp` file beside the database, so a restarted `serve` doesn't run it again.
  - The stamp is written first, so a cleanup that fails isn't retried every few seconds. Its problem is reported once.
  - `serve --drain` doesn't clean up.
- **Configuration.** The new `[cleanup]` table in `config.toml` takes:
  - `automatic = false` to turn it off;
  - `after = "7d"` (or a duration such as `"36h"`), 7 days by default. A value that isn't a positive duration is refused by name.
- **A bug fixed in `clean`.** When a merged branch's worktree was kept for untracked files or edits, clean still deleted the branch checked out in it. That left the worktree on a branch that no longer existed. A kept worktree now keeps its branch, both in the preview and if the worktree becomes dirty while clean runs.

## Not done, and why

- **`clean` for closed or archived branches.** Their work isn't merged, so only the worktree could go. Nothing can check a removed worktree out again yet: `path`, `check`, and the other verbs would find it gone. This waits for a way to recreate a branch's worktree.
- **Decision 36's other targets.** None exists in v3 yet:
  - the GitHub log caches, build archives, and kept-failed Tart VMs;
  - `tart delete` of dockhand's pulled images;
  - the free-space trigger (`cleanup.min_free`);
  - pruning run logs with their runs.

  Each joins `Cleanup` when the thing it cleans lands.

## Tests

- **Config:** defaults, `automatic = false`, days and hours, and three bad values.
- **Engine:** a merged branch whose worktree has an untracked file, and an index cache with an old and a fresh generation.
  - Cleanup removes the fork's branch and the old generation.
  - It keeps the worktree, its branch (still checked out), the file, and the fresh generation.
  - A second run removes nothing.
- **Command:** a merged branch and `serve`:
  - with cleanup off, nothing is removed;
  - on, it reports and removes the worktree, branch, and fork branch, and writes the stamp;
  - a second `serve` the same day cleans nothing.
