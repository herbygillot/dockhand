# 2026-09-25: renamed branches

Design v3 §2: "A branch renamed with Git keeps its identity through `adopt`'s reconciliation", and decision 8 of §14: the branch record's identity is separate from its ref name.

## What changed

- **`adopt` recognizes a rename.** When the branch being adopted isn't tracked, adopt looks for a tracked branch whose Git branch is gone and that either:
  - has its worktree now checking out the adopted branch, as `git branch -m` leaves it; or
  - has its last push to its pull request contained in the adopted branch.

  If there is exactly one such branch, its record takes the new name. Its ID, base, checks, checkpoints, and pull request carry over, and the change is journaled as `branch.rename`. If two could match, dockhand won't guess between them, and says so.
- **A pull request's head never moves**, so `submit` keeps pushing to the fork branch the pull request was opened from, whatever the local branch is now called. The new `SubmitPlan.RemoteBranch` is used for:
  - the fork's head;
  - the pull request search;
  - the conditional push;
  - the pull request's input.
- **Status.** A branch whose Git branch is gone now suggests `dockhand adopt <new name>, if you renamed it`, where it used to suggest recreating the branch.

## Tests

- **Engine:**
  - A submitted branch renamed with Git is adopted under its new name with the same record and pull request. A second adopt finds it tracked. A new commit then pushes to the fork's original branch, and no branch with the new name appears on the fork.
  - An unrelated branch adopted while a renamed one is missing gets its own record. The renamed one is then recognized.
- **Command:** after the rename, `status --attention` suggests adopt. `adopt` says what it recognized and where submit keeps pushing, and status shows the pull request again.
