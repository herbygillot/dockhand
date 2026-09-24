# 2026-09-24: amend refuses a wrong checkout before accepting a job

Found while realigning the `cargo.crates` blocks of three open pull
requests (xan #34844, kasane #34849, fnm #34857) with `amend`, from a
worktree of the ports repository on each contribution's branch.

- With `MACPORTS_TREE` naming the main checkout, which was on master,
  `amend` refused with only "workflow: selected revision is stale"
  (`workflow/correction.go`). The checkout it captures was on another
  branch, which is not a stale revision. It now says the checkout's path,
  the branch it is on, and the contribution's branch, and to check that
  branch out there or name its checkout with `--tree`; a HEAD that
  differs from the branch's is still a stale revision, and says both
  commits.
- With the realigned Portfile written but not staged, `amend` accepted a
  job, which integration then refused ("checkout/index changed or edits
  are unstaged"), leaving the job needing attention. Integration commits
  what is staged, so binding now refuses first, naming the unstaged files
  (`git.Repository.UnstagedPaths`, `git diff --name-only`).

`TestAmendOfTheCheckoutRefusesAWrongBranchOrUnstagedEdits` covers both and
that no job is left needing attention.

Also found, and left as the design has it: `bump` on an open contribution
at the release it already carries continues the recorded source and
republishes it (`Continuing contribution … from recorded source`),
reusing its verification, so a fix to how dockhand writes a Portfile does
not reach an open pull request that way. `amend` with the rewritten file
staged does, and so will the changeset commands of step 9.
