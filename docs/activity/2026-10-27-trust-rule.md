# The trust rule in code

2026-10-27, Then's item 12, its first part (the direction synthesis's
"Before D", the person's word of 2026-10-02).

- **Whose Portfiles**: `engine.OthersPortfiles`. An adopted pull
  request's are its author's, "@alice's". Its author is now kept
  (`PullRequest.Author`, schema 49, `branches.pr_author`); one adopted
  before reads "#4711's author's".
- **Who's watching**: `engine.Unattended(ctx)` marks work nobody watches:
  - serve's loop (`Serve`);
  - ship and bump where nobody was asked or --yes answered (`ship`);
  - any command with no terminal, a script's (the root command).
- **The gate**: `mayEvaluate`, and `mayEvaluateOthers` for review's
  pull request. It is placed at each evaluation of a branch's Portfiles
  on this Mac:
  - change records (`revisionChanges`, where adopted branches had been
    skipped outright);
  - assessments (`revisionAssessments`, which had evaluated an adopted
    branch's unsaid; prioritization's ruling, 2026-10-27: no harm for
    v0.6.0, a known issue for its notes);
  - a check's plan (`ProposeCheck`, refused unattended with how a person
    checks it);
  - the link scan (`UndeclaredLinks`);
  - the evaluating Portfile rules, restated's and path-style's
    (`portfileFindings`), in tidy, submit, and review.

  Attended, it says once "Evaluating @alice's Portfiles on this Mac, for
  pr-4711". Unattended, it skips the step, says "pr-4711's check skipped:
  nothing unattended evaluates @alice's Portfiles on this Mac", and
  journals it (`branch.trust-skipped`).
- The experimental notice of review and adopt --pr is gone. The README
  and docs/usage.md say the rule instead.
- Not yet: `review <pr> --check`. `trust.others = "guest"` waits in Later.
- Tests: TestAnAdoptedPullRequestsRecordWaitsForAPerson,
  TestNothingUnattendedChecksAnAdoptedPullRequest, and review's two.
