# Hygiene, the parts PR #4 doesn't touch (v0.4.0 part 1)

PR #4 (herbygillot/dockhand#4) is still open, and most of part 1's hygiene
waits on it, as the split's coalescing placed it. These three open no file
it touches.

- **The store's pruning moves to `store/sqlite/prune.go`** (the SQL
  study's split): the archive keep-sets (`reusableBuilds`,
  `openBranchArchives`, `newestDependencies`, `unnamedArchive`),
  `PruneArchives`, `PruneHistory` with its chunked, foreign-key-ordered
  steps, and `PruneAssessments`, apart from the records they prune.
  records.go goes from 1,072 lines to 852. No code changed.
- **`CleanStep.Kind` is exported** (the command package review), as
  `engine.CleanKind`: worktree, branch, fork, or check. command's clean
  read a branch step by `strings.HasPrefix(s.What, "branch ")`; it reads
  the kind, and `clean --json`'s steps carry `kind`.
- **`tart.Status.HasHostImage`** (the command package review): setup's
  private `hostImage` is buildenv/tart's Status's own method.

Before the alias and bridge deletions, the roadmap asks for one `clean
--archived` and one `clean --legacy` preview on the person's Mac. Both ran
here with a v0.3.1 build (the database is schema 31, and main's 32 would
have migrated it), removing nothing: `--archived` would take 13 archived
branches' worktrees and Git branches, 9 of them having nothing master
lacks and 2 named for a look (zola and trivy, whose ports master has at
other versions); `--legacy` sorts 8 branches from before v3, codex's for a
look and 7 kept, each for `dockhand adopt`. Once bridge 1 goes, those 8
stay plain Git branches.

Tests: TestJSONEnvelopes (a clean step's kind), TestAStatusHasTheHostsImage.
