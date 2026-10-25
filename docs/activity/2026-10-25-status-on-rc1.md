# status on rc1: its remotes once, and no worktree checked out

2026-10-25, from v0.6.0's acceptance on rc1, placed at roadmap 426d2252
as fixes for main.

- **The remotes once.** On 683 branches `status --all` ran about 7,372 git
  processes, 3,415 of them `git remote get-url`: each branch's gathering
  asked `hasForkRemote`, five processes for the checkout's two remotes.
  engine `Status` now reads it once and hands it to each branch
  (`branchStatus`); `BranchStatus` alone still reads it. `git remote -v`
  in place of `get-url` was tried and left: it shows a remote's first
  fetch URL alone where `get-url --all` shows each, so a remote with two
  wouldn't be refused as it is. The rest of status --all's time is Then's
  item 6's.
- **No worktree checked out.** Since `--hand-edit`, status read a branch's
  hand edits from its working files through `e.worktree`, which checks a
  managed worktree out again where it's gone, an archived branch's too.
  It now opens the worktree (`openWorktree`), its committed files standing
  where it's gone.
- **Known issues for v0.6.0**, if it ships on rc1: `status --all` takes
  about a tenth of a second a branch, over a minute with several hundred,
  most of them archived; and `status` checks out again a removed worktree
  of a branch `update --hand-edit` started, which `clean` removes again.
  They go in the README's v0.6.0 known issues when that section is
  written for the release.
- **Tests:** TestStatusReadsTheRemotesOnce, and the removed worktree in
  TestAHandEditWritesTheAttemptAsThePersons.
