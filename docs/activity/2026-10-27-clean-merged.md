# clean removes a merged branch whose changes are on master

2026-10-27, field testing's note read against 1186127b (roadmap 84e8e39b).
xapian-core-dsq3, net-snmp-5.9.5.2, and wasmedge-0.18.1, each retidied
after its submit, were kept on every clean, "it has commits beyond what
was merged", though master had every file they change; status called
each "cleaned"; and pr-34539's fork branch, which no remote pushes to,
was listed on every clean with no way to end it.

- **By content** (engine/clean.go `changesOnMaster`): where a merged
  branch's tip isn't the merged commit, but every path its commits change
  from its base is at its tip as the master dockhand last fetched has it,
  its worktree and Git branch go, the branch leased to its tip, saying
  "its changes are on master". One with a change master lacks stays.
- **status** calls a branch cleaned only once its Git branch and the
  worktree dockhand made are both gone (`WorktreeStays`), and a merged or
  closed branch clean kept reads as what it kept and why
  (model.Branch.CleanKept), never "cleaned".
- **A step no clean can take** (`CleanStep.Said`), a fork branch no remote
  pushes to, is said by the clean that first keeps it and recorded, stays
  on status --all's line, and isn't listed again; a plan of only such
  steps isn't listed.
- **clean --delete** forgets a merged or closed branch as it does an
  archived one, an open one still refused, so a person can end a listing.

TestCleanRemovesARetidiedBranchWhoseChangesAreOnMaster and
TestAnUnreachableForkBranchIsSaidOnce; TestCleanKeepsWorkOfItsOwn still
keeps a branch with a change master lacks.
