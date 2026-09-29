# 2026-09-29: status changes nothing

The hugo exercise's session found `status --all` re-creating a worktree ([review](../reviews/2026-09-28-hugo-bump-exercise.md#cleaning-up-after-beekeeper-studio-and-ov), finding 2). `clean --archived` had removed the worktree of duckdb-cxx14, an archived branch. The next `status --all`, an observer, checked it out again, and the journal said "checked out again in …/duckdb-cxx14". That undid `clean --archived`, and a command that only reads made a worktree.

A branch's status read its worktree through the engine's `worktree`, which checks out again a managed branch's worktree that clean removed, as work on the branch should. Status isn't work on it. Now `openWorktree` opens a worktree as it is, and refuses one that isn't there, and status reads through it. Where the worktree is gone, a branch's files are its committed ones, as for a branch never checked out. `worktree`, for work on the branch, still checks it out again first, and then opens it the same way.

The same status asked "! duckdb-cxx14 snapshot 1 passed; the files have changed since", nudging a check of a branch the person had set aside. The worktree's re-creation didn't cause that: the person had discarded the branch's only edit, so its files had moved on from what passed. But an archived branch's checks ask nothing of the person now. Its stopped runs, its pull request, and a Git branch gone still do.

Tests:
- `TestStatusDoesntCheckOutAgainWhatCleanRemoved`: an archived branch's status after clean, which leaves the worktree gone and reads the committed files, then an edit on the branch, which checks it out again;
- `TestAnArchivedBranchAsksNothingOfItsChecks`.

Three mutations each fail a test. One of them, `worktree` no longer checking out again, no test caught before this one.
