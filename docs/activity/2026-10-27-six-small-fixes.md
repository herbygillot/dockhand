# Six small fixes for rc6

2026-10-27, at the person's "Take the six." (03:16Z, in the project
chat), from v0.7.0's lead into rc6, ahead of the language review's sweep.

- **A baseline keeps the check's variants** (the docs dig's I6). The
  baseline planned master with each target's ID as an `Also` name, and a
  variant build's ID, "libharbor +docs", names no port, so a `--variants`
  check's baseline built the default build alone, or refused where only a
  variant build failed. `PlanBaseline` now hands `PlanCheck` each variant
  target, and `alsoVariants` (engine/plan.go) evaluates it with its own
  variants in each environment, as `planVariants` does, leaving it out
  where master's port doesn't declare them. Done:
  TestABaselineKeepsTheChecksVariants plans "libharbor" and "libharbor
  +docs" after a `--variants each` check where both failed and +tests
  passed; without the change it planned "libharbor" alone.
- **A subport the branch adds is a subject's port** (I21). `ruleCommits`
  (engine/tidy.go) reads the ports each directory defines at the branch's
  head as well as at its base, for tidy, submit, and review, so
  "terraform-1.17: new subport" passes `subject-port` on the branch that
  adds it, and a subject naming no port of the directory is still
  flagged (TestASubjectMayNameTheSubportTheBranchAdds). review reads the
  head's index once more for it (TestReviewStartsAtThePullRequestsOwnBase).
- **An explicit prerelease on a stable port is said** (I32). `update`
  says "Prerelease: this takes jq out of stable (1.7.1 → 2.0-rc1).",
  `--json` carries `leaves_stable`, and submit's preview repeats it once,
  as "! this takes jq out of stable (1.7.1 → 2.0-rc1)"; it holds nothing
  (`engine.LeavesStableWords`, SubmitPlan.Prereleases). The test bumper
  now records the version it compared against, as discovery does.
  TestAnUpdateOutOfStableIsSaid.
- **git's ssh never asks under serve** (I46). `git.Unattended` gives git
  an ssh command with `-o BatchMode=yes`, keeping the person's
  GIT_SSH_COMMAND or core.sshCommand and leaving GIT_SSH's program alone;
  compose applies it where nobody can answer: serve, at a terminal or
  not, and any command without one on standard input, as a background
  worker. A key no agent holds then fails at once as the SSH refusal
  dockhand already words (TestUnattendedSSHNeverAsks).
- **`-v` prints a list whole** (I37). `view.ListShown` is eight, or every
  entry at `-v`, for a plan's Changed, Siblings, Also, Order, and Excluded
  lines and update's library dependents (TestVerboseNamesAWholeList).
- **Two small corrections** (I39, I44's middle part). The copy advice
  names `sqlite3 <database> ".backup <file>"`, since a plain copy of a
  database in use can miss its last writes; the unreadable database's
  says it with the path. Tart's and local's capabilities are asserted at
  compile time, as script's and GitHub's were.

The language review's sweep then read command, engine, branchstate, and
the providers for its recurring phrases: adopt's short help is "Track an
existing branch without changing it", the Generated-By build lines say
"which others cannot find until it is pushed there" and "so its source
cannot be identified", and GitHub's missing allowance says "GitHub's
response did not include the hourly request allowance". The six's own
words read as the review asks.
