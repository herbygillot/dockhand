# Status gathered once

2026-10-27, Then's item 6 (C8-6, with its speed).

- **One report** (engine/statusreport.go `StatusReport`, `StatusRequest`):
  each branch's status judged once, a stopped check marked; for a branch
  shown in detail, what its latest check's builds link and don't declare;
  and, for the overview, serve's state, the queue, the processes, what
  changed in the providers, and serve's last look. status writes its text
  and JSON from it alone: under `--json` it judged a branch twice, read
  `UndeclaredLinks` while writing it, and opened the observer three
  times. view's `WriteNextFor`, which read and judged a branch itself, is
  gone: check reads `engine.BranchStatusOf` and writes `view.WriteNext`.
- **Every branch's Git branch read at once** (`statusReads`,
  `readBranches`): heads by one `for-each-ref refs/heads/`, every head's,
  base's, and master's tree by one `cat-file --batch-check`, the worktrees
  each branch is checked out in by one `worktree list` (git
  `AllCheckouts`, opened with `Repository.Worktree`, where each branch
  ran rev-parse twice and symbolic-ref), commit counts by one
  `for-each-ref %(ahead-behind:<base>)` a base where the Git is 2.41 or
  newer (`CountsFrom`; older counts a branch at a time), and what each head
  changes from its base, and an open one's from master, by one
  `diff-tree --stdin` (`ChangedPathsOf`), kept as analyze keeps its own.
  followRenames reads the heads once too, where each open branch was two.
  What stays a branch at a time is each worktree's own `git status`.
- **An ended branch**, merged, closed, or archived, skips its holds, hand
  edits, and network holds (`readOpen`); whether it landed was open
  branches' alone already. Its files are still read.
- In the fixture, 6 branches went from 62 git processes to 18, and the
  regression test (TestStatusOfAllRunsAFewGitProcessesWhateverItsBranches)
  has 2 branches and 8 with changes run the same git processes but for
  each worktree's status. The done line's M1 measure of dhtest's
  `status --all` against rc2's 55 s is the M1's to make.
