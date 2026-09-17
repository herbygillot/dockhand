# Merged contributions clean up their branches

A merged pull request ends a contribution, and the branches behind it are residue. Until now `refresh` retired the contribution and left both the local branch and the fork's head branch in place, and the ports tree had accumulated eleven `dockhand/*` branches over a week of exercises.

## Behavior

When `refresh` observes a merged PR and retires the contribution, it deletes the local branch and the fork's head branch. Each deletion is guarded: the local branch must still point at the published commit and must not be checked out in any worktree; the fork branch must still hold the merged head, checked with the same compare-and-swap lease the push path uses, and is reached through whichever local remote pushes to the PR's head repository. Every outcome is reported in the refresh result, and none of them fails the retirement: a kept branch says why. A closed PR keeps its branch, because the work may resume.

`gc` sweeps local branches of merged contributions whose branch survived, such as those retired before this existed or checked out at the time, deleting a branch only while it holds the published commit and is not checked out, and reporting the rest. Fork branches are removed at merge observation only. The maintenance engine now opens each registration's checkout so the sweep can run; a registration without a checkout still collects resources.

`git.DeleteRemoteBranch` and `Repository.Checkouts` are the two new primitives.

## Validation

- The retirement test now asserts that a merged PR leaves neither the local nor the fork branch, while a closed PR keeps the local branch.
- A new test checks the branch out in a separate worktree, observes the merge, and checks that the local branch is kept with the checkout named while the fork branch is deleted; that `gc` reports the checked-out branch, deletes it once the worktree is gone, previews under `--dry-run`, and keeps a branch that moved past the published commit.
- Live: `gc --dry-run` on the ports tree listed exactly the four branches of merged contributions, and `gc` deleted them, leaving the seven that belong to open or abandoned ones.
