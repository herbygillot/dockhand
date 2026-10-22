# Forgetting an archived branch

Roadmap part 1, item 8. `clean --delete <branch>...` forgets archived
branches named.

The record can't simply go: other branches' results name its executions
in `results.reused_from`, a text column, not a foreign key, and an
execution's run, revision, and plan reach the branch by foreign keys. Of
the roadmap's two ways, keeping the reused executions alone or a
forgotten state, I took the second, as a mark: schema 34 adds
`branches.forgotten_at`, rather than a `forgotten` state, since SQLite
can't change the state column's CHECK without rebuilding the table the
others refer to. The unique indexes on a branch's name and pull request
leave forgotten branches out, so both are free for another branch.

- `Store.ForgetBranch` (prune.go) refuses all but an archived branch,
  once; deletes its events, edits, acceptances, and checkpoints; prunes
  its checks as history pruning does, keeping what reuse may still choose
  or another result names (`pruneBranchHistory`), and then its latest
  run and the plans and revisions left with nothing; and marks it.
  Every list of branches and every lookup by name leaves it out;
  `Branch(id)` still reads it, with `ForgottenAt`.
- `engine.PlanForget` is what goes: the worktree where one is left, the
  Git branch at its commit, and the commits on it the master last fetched
  lacks (`git cherry`, a rebased merge's commits not counted).
  `engine.Forget` removes the worktree, deletes the Git branch only while
  it's at that commit, and forgets the record.
- clean words the plan, "! 1 commit master doesn't have, deleted with
  it:", asks, needs `--yes` without a terminal, and says each branch
  forgotten; `--json` gives `forgotten`.

With it, Later's "status says what clean kept": the same migration adds
`branches.clean_kept`, which `ApplyClean` sets from the steps it kept, as
"fork branch kept: it moved while clean ran", and `status --all` says
after "cleaned", and `status <branch>` on a line of its own.
