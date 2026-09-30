# 2026-09-29: baselines and test evidence

Batch 7 of the roadmap's smaller items. A baseline is how a person tells a branch's failure from master's, and in the libuv run tests were what it was run for.

**A baseline reads advisory test results** (the libuv run's finding 5). check-38's tests failed where the policy only reports them, so its ports passed. `check --baseline` said "nothing failed in check-38, so there is nothing to compare"; with `--only uvw,uvw2`, it compared outcomes only, "✓ builds at the base, as it does on the branch", though uvw's tests failed at the base too and uvw2's passed there. Now:
- a port whose tests failed or timed out, though the policy let it pass, is worth a baseline (`BaselineWorthy`), is rebuilt where its tests failed (`rebuildWhere`), and a failed-tests check points to the baseline as a failed one does;
- where both builds passed and either's tests failed, the baseline's line says what the tests did at the base beside the branch: "its tests fail at the base too, as in check-38, so they did before this branch", "its tests pass at the base, and time out in check-38; the cause isn't established", or that they weren't run on one side.
- `model.TestOutcome.Failed()` names tests that failed or timed out, which the test policy's judge now uses too.

**`check --baseline --plan`** (the libuv run's finding 4) was refused by cobra's flag group rule, in cobra's words. It now shows the baseline it would run, its header and plan, and runs nothing. Nor does it record the base as a revision of the branch, as planning a baseline to run does: `PreviewBaseline` uses the base's revision where one is recorded, and otherwise one with an ID of its own, unrecorded, as a check's `--plan` captures.

**A port that declares no tests, under `--tests required`** (the ov run's finding 3). ov declares none, so it passed under any policy, as designed, but the plan said "tests required" and the grid "✓", as for tests that passed. Whether a port declares tests was only the guest's to read. Base declares `test.run` with no default and reads it with `tbool`, so an unset one is off, and the evaluator reported it only where set. It now reports MacPorts' own reading, `tbool test.run`, as `dockhand.test_run`. The planner records the targets that declare none in each environment (`EnvironmentPlan.Untested`), where the option was read. A plan requiring tests names them, "No tests    ov declares none, so requiring them asks nothing of it", and a passed result with no tests reads "✓ declares no tests" under that policy.

Tests:
- `TestABaselineTakesPortsThatFailedWhileBuilding`, with advisory failures and where they're rebuilt, and `TestABaselineSaysWhatTheTestsDidAtTheBase`;
- `TestBaselineComparesWithTheBase`, with `--baseline --plan` running nothing, and `TestABaselineUsesTheCheckedBase`, recording nothing;
- `TestTheEvaluatorReadsWhetherAPortDeclaresTests`, `TestAPlanRecordsWhatDeclaresNoTests`, and `TestAPlanRequiringTestsNamesWhatDeclaresNone`.

Twelve mutations each fail a test.
