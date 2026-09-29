# 2026-09-29: clean keeps a branch checked out where it stays

The hugo exercise's session found a hazard in `clean` ([review](../reviews/2026-09-28-hugo-bump-exercise.md#cleaning-up-after-beekeeper-studio-and-ov)). `hand/ov-0.55` was a branch the person made by hand in a worktree of their own, then adopted. Once its pull request merged, `clean --yes` removed the branch and the fork's branch, and rightly left the worktree, which dockhand didn't make. But the branch was checked out there. The worktree was left on a branch that was gone: `git status` said "No commits yet", and every file showed as added. Nothing was lost that time, but in a person's own worktree that's a real hazard.

The branch step kept a branch only where it was checked out in the person's own checkout. It never asked about other worktrees, and it deletes the branch through `update-ref`, which, unlike `git branch -d`, doesn't refuse a branch that's checked out.

Now a branch checked out anywhere clean doesn't remove is kept, and says where (`checkedOut`):
- in the person's checkout, "it is checked out in your checkout; switch away first", as before;
- anywhere else, "it is checked out in ~/…/ov-hand; switch away there first".

The worktree clean itself removes, a managed branch's, doesn't count. The branch is looked for when the plan is made, for the preview, and again just before it goes, when that worktree is gone, so one checked out in between is kept too.

The guide's account of `clean` says so.

`TestCleanKeepsABranchCheckedOutWhereItStays` covers an adopted branch's worktree, the person's checkout, and a checkout made after the plan. Four mutations each fail a test, one of them in the existing test of a managed branch, whose worktree is removed first.
