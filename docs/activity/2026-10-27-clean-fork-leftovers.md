# clean offers what closed and canceled work left on the fork

2026-10-27, the docs dig's I31 (roadmap's "clean offers the fork branches
closed and canceled work left"). Only a merged branch's clean reached its
pull request's head on the fork and its `dockhand-check/<sha>` branches,
so a pull request closed unmerged, a forgotten branch, and a canceled
check left theirs for good.

- **One planner for the fork** (engine/clean.go `planCleanFork`): the pull
  request's head while it holds what dockhand pushed (`pushedHead`, the
  head last observed, else the one pushed), and the check branches its
  GitHub checks left, each while it holds the commit checked. The merged
  path uses it with the merged commit; closed and archived branches
  (`planCleanWorktree`, its local part now `planCleanLocal`) with the
  commit pushed, one moved kept "it has moved since dockhand pushed it".
- **A branch with no pull request** has its check branches looked for on
  each remote that pushes to a GitHub repository other than MacPorts'
  (`forkRepositories`), one that can't be read said nothing of where
  there are several.
- **clean --delete** (`ForgetPlan.Fork`) lists them, removes them before
  the record whose checks name them is forgotten, and says any kept.
  `Forget` takes the plan by pointer so the kept steps come back.
- clean's help and usage.md say it; writeClean's "dockhand path checks it
  out again" line now reads a worktree step alone.
- TestCleanOffersWhatClosedWorkLeftOnTheFork: a closed branch's fork head
  and check branch removed, its Git branch with work kept; one moved since
  kept and said; an archived branch never submitted, forgotten, loses its
  check branch.
