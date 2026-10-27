# The trust rule in code

2026-10-27, Then's item 12, its first part (the direction synthesis's
"Before D", the person's word of 2026-10-02).

- **Whose Portfiles**: `engine.OthersPortfiles`. An adopted pull
  request's are its author's, "@alice's". Its author is now kept
  (`PullRequest.Author`, schema 49, `branches.pr_author`); one adopted
  before reads "#4711's author's".
- **Who's watching**: `engine.Unattended(ctx)` marks work nobody watches:
  - serve's loop (`Serve`);
  - ship and bump where nobody was asked or --yes answered (`ship`,
    `shipOptions.within`).

  This is how dockhand reads unattended everywhere: the holds, the hand
  edit, the fuzz refresh. A command with no terminal is a person's, or an
  agent's at their word, and says it as any attended command does. The
  first cut took a run with no terminal as unattended. The prioritization
  thread dropped that at its card's recommendation (2026-10-27), pending
  the person's word: it would refuse every agent's check of an adopted
  pull request, and the evaluator can't write, reach the network, or run
  programs either way.
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
- **`review <pr> --check`** adopts the pull request as adopt --pr does,
  or takes the branch it was adopted as, and checks it in Tart, or where
  --on says, reporting as check does. Its plan evaluates the author's
  Portfiles on this Mac, which it says; the builds run in the guests. A
  script that posts nothing ends with the check's answer.
  `trust.others = "guest"` waits in Later.
- Tests: TestAnAdoptedPullRequestsRecordWaitsForAPerson,
  TestNothingUnattendedChecksAnAdoptedPullRequest,
  TestShipIsUnattendedWhereNobodyLooks, TestReviewCheckAdoptsAndChecks,
  and review's two others.

## Its unattended half leaves (2026-10-27, later)

At the person's word on the docs consolidation's re-review ("Yes, I
agree.", 15:42Z), the trust rule keeps its attended half alone. Its
unattended half served the maintainer inbox, which isn't taken, and
nothing else unattended evaluates another's unmerged Portfile.

- **Gone**:
  - `engine.Unattended` and its mark, set by serve and by ship's `within`;
  - the skip and its words;
  - `branch.trust-skipped` (`EventTrustSkipped`);
  - `MayCheck`, with its calls in `ProposeCheck` and review's `--check`.

  `mayEvaluate`, which always answered yes after that, is now `sayWhose`,
  which only says it, and `portfileFindings` takes no `evaluate`.
- **Kept**: `OthersPortfiles` and `branches.pr_author`, change records
  and assessments for adopted branches, `review <pr> --check`, and
  `local`'s refusal of an adopted branch.
- **Docs**: the README, docs/usage.md, and the release-notes drafts say
  it. v0.7.0's Upgrading loses its line on a command with no terminal,
  which only the trust rule read.
- Test: TestAnAdoptedPullRequestsRecordSaysWhose, with review's two.
