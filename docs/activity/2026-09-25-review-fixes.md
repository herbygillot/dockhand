# 2026-09-25: the implementation review's four findings

The [2026-09-25 implementation review](../reviews/2026-09-25-v3-implementation-review.md)
found four gaps in v3's check, tidy, and submit guarantees. Its probes
([patch](../reviews/2026-09-25-v3-regression-probes.patch)) reproduced
each on the Mac, and each contradicted Design v3's own wording. They are
fixed here, before the Tart provider, whose acceptance journeys run
through all four.

## 1. A narrowed check never shrinks what submit requires

`check --only jq` on a branch that also changed libharbor let `submit`
plan a ready pull request: the evidence was the last run's targets alone,
and `--only` had narrowed `Plan.Targets` in place, keeping no record of
what it left out (§7: "A narrowed check never quietly shrinks what
`submit` requires").

- **The plan keeps its changed scope.** `model.Plan.Omitted` records the
  changed targets `--only` left out, and `check` prints them: "Left out
  libharbor, by --only; submit still needs them checked". Plans are JSON,
  so no schema change.
- **Evidence is the tree's, not one run's.** `treeEvidence` starts from
  the newest finished check of the files and judges it against everything
  its plan required, the omitted targets included. It fills what that
  check didn't build from earlier checks of the same files, newest first,
  environment by environment, since a result holds for its tree. So a
  narrowed check after a full one keeps the full one's results, instead
  of superseding them. `Evidence.Earlier` names the checks it drew on,
  and the pull request says so: "Checked by dockhand check-3, with
  results from check-2 for the same files."
- **What nothing built blocks.** A target no check built everywhere it is
  required is `Unchecked`. A changed one blocks submission ("no check of
  these files built it; dockhand check builds it, or share the branch as
  a draft"), can't be `--accept`ed, since there's no failure to accept,
  and shows as "· not run" in the pull request's table.
- **One predicate.** `status` uses the same tree evidence.
  `PassingBranches`, which `submit --passing` and serve both read, now
  also requires that nothing failed or went unbuilt. The attention list
  says "check-2 passed, but no check of these files built libharbor".

`TestANarrowedCheckNeverShrinksWhatSubmitRequires` is the review's probe,
extended:

- **The narrowed check alone.** Submission is blocked, `--accept` is
  refused, the pull request shows the row, and `PassingBranches` doesn't
  count the branch.
- **Full check, then narrowed.** Submission is clear, and the evidence
  cites the full check.

`TestCheckPlanNamesWhatOnlyLeftOut` covers the plan's line, and
`TestPlanValidation` refuses a target both planned and omitted.
