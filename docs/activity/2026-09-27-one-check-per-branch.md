# 2026-09-27: one check per branch, wherever a check is queued

Decision 29 gives a branch one check at a time, but only `check` kept it, through `replaceActive` in the command. `submit --check`, and through it `update --submit` on an existing branch, queued a second check beside a running one. So did `retry`, whose run was written apart from `Enqueue`. The code-organization review's finding 2 named the first and the last; the check of it found the second.

**What changed.**
- **The engine keeps the rule.** `Enqueue` and `Retry` record a run through one `queue`, which refuses, inside the same transaction, while another of the branch's checks is queued or running. The error is `ActiveRunError`, naming the run and whether it checks the same files:
  - "check-1 is already queued for these files; dockhand wait check-1 follows it";
  - "check-1 is running for commit abc1234, and a branch has one check at a time; dockhand cancel check-1 stops it, keeping what it finished".
- **Exceptions.** A baseline looks beside the check it explains, and doesn't count. Neither does a run asked to stop, which is on its way out; so `check --replace` queues its check while the one it stopped finishes stopping. `check`'s own refusal, before it captures, skips such runs too, so the two agree.
- **`submit --check`** adds how to finish when a check of the same commit is queued: "once it passes, dockhand submit --branch jq-update submits this commit".
- **`--tests` is checked before anything is captured.** A misspelled policy was refused only after capture and evaluation, as "plan plan_… has unknown test policy". `model.TestPolicy.Valid` is the one test, used by the plan's validation, `check`, and `PlanCheck`.
- **Serve's ordering test** queued three checks on one branch, which the rule now forbids. It records two other branches' checks instead, the state serve's queue actually holds.

**Tests.**
- `TestABranchHasOneCheckAtATime` covers:
  - the same files, and others;
  - a baseline beside;
  - a retry while another check is queued;
  - a check asked to stop.
- `TestSubmitCheckKeepsToOneCheckAndItsCommit` covers the refusal and its hint, and submit's commit binding, which had no test at all: a build that commits to the branch as it runs, so what passed isn't what submit would push, and nothing is submitted.
- `TestCheckRefusesAnUnknownTestPolicyAtOnce`.
- Removing the guard or the binding fails them.
