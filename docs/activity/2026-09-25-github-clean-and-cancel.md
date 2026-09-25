# 2026-09-25: the github provider cleans up and cancels

Two follow-ups to the [github provider](2026-09-25-github-provider.md).

## What changed

- **`clean` removes check branches.**
  - For a merged branch, clean also lists the `dockhand-check/<commit>` branches its github checks pushed to your fork, one per commit checked. A snapshot's commit is read from `refs/dockhand/revisions/`, before clean drops those refs.
  - Each is removed only while it still holds that commit, with the same conditional delete as the PR's branch. One that moved is kept, with the reason.
  - They are looked for in the repository the pull request came from, which is the fork the provider pushes to.
  - `engine.CheckBranchPrefix` names them once, for the provider and for clean.
- **Cancelling a check cancels its run on GitHub.**
  - `engine.Build` gained `Canceled()`. Once the context is done, it says whether the run was canceled or interrupted for good, or whether its driver is only stopping with the run left for the next one. A `serve` that stops is the second case.
  - The provider cancels the Actions run only in the first case, and only if the run hasn't finished. It does this best effort, within 30 seconds, on a context that outlives the canceled one.
  - A stopping `serve` leaves the run going, so the next driver finds the same run and reads it, instead of building again.
  - GitHub answers a cancel with 202 Accepted, which go-github reports as an error. The client treats it as success.

## Decisions

- **Only merged branches**, as with the rest of clean. A branch archived or closed without merging keeps its check branches, as it keeps its worktree.
