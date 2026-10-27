# After submit: a follow-up branch, CI's error line, and a merge conflict

2026-10-27, Then's item 7 (the reflections' F, the exiv2 session's per-port
CI, and the docs dig's I2).

- **start --from <branch>** (engine/followup.go `StartFrom`): a merged
  branch's commits past what its pull request merged (`PastMerge`: past
  the head last observed, else pushed, and not where master has every
  change by another route, as clean reads it) are replayed onto master
  fetched now, as rebase replays (git `Replay`), and the branch starts
  there (StartRequest's new `Head` and `Directories`, the ports they
  change checked out). A commit master has the change of is dropped; a
  conflict, or nothing past the merge, starts nothing. Status counts them
  on a merged branch (BranchStatus.PastMerge) and its row is
  `work-past-merge`, "#35123 merged; 1 commit past the merge isn't in
  it, for a follow-up", Next `dockhand start --from <branch>` (a new verb,
  `start`). With no name the branch is the merged one's with a short ID.
- **A merge conflict is said** (the docs dig's I2): the refresh's GraphQL
  query reads `mergeable`, which reading.go set "unknown" whatever GitHub
  said; the observation keeps `Conflicts`, and status and the attention
  list say "#123 conflicts with master", Next `dockhand rebase` (a new
  verb). UNKNOWN, until GitHub has computed the merge, is no conflict
  until a later look.
- **CI's error line and each port** (schema 44, `ci_failures.error` and
  `ports`): a failed job that wasn't a fetch keeps its log's first error
  line, read with buildlog.FirstFrom on the log with GitHub's timestamps
  taken off (ghactions `Untimed`), else the workflow's own `##[error]`
  marker (`FirstError`); and each port the job built, as the workflow's
  markers say (`PortResults`, from ReadLog). The CI row says it: "#35101:
  MacPorts CI failed: on macos-15: src/garmin.cc:120:5: error: … (CI:
  exiv2 ✓, gpsbabel ✗ (build))". The engine may import
  buildenv/ghactions for these readers of MacPorts' workflow log.
- Tests: TestAFollowUpStartsFromTheCommitsPastAMerge,
  TestAFailedCIJobSaysItsFirstErrorAndEachPort, the reading test's
  `mergeable`, and the attention test's conflict row.
