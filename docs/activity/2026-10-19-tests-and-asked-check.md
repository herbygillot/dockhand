# check.tests resolved once, and submit's plan judges the check asked

Two more lines of the roadmap's v0.5.0 part 1 item 4, "One owner for each
rule", from the architecture review of 5897a82.

## R3-35: `check.tests` resolved in `ProposeCheck`

`cmp.Or(tests, s.file.Check.Tests)` was merged at check, at ship's
`queueHead`, and for serve through `ServeOutdated.Tests`.

- `engine.Options.Tests` is check.tests, set where the engine is opened
  (command/settings.go); `ProposeCheck` takes it where `CheckRequest.Tests`
  is empty.
- Call sites switched: check.go's merge, `queueHead`'s, and serve's
  (`ServeOutdated.Tests` is gone; serve's `PrepareOptions` sets none).
- Grep for the old shape: `Check.Tests` is read by config, `config`'s
  listing, and settings.go only.
- Test: `TestACheckAskingNoTestsRunsTheConfigs`.

## R3-8: submit's plan says whether a check stands for this ask

`!plan.CheckNeeded && !place.Explicit() && tests == ""` at
command/submit.go glued two engine judgments: `CheckNeeded`, that a check
of the tree exists, and `Covered`, that it covers the plan asked.

- `SubmitRequest.Where` and `Tests` are where and with what tests ship's
  check was asked to build. For a `PendingCheck` plan whose check finished,
  `PlanSubmit` judges an ask of either by `coversAsked`, the plan of the
  same revision `Covered` compares with, and sets `CheckNeeded` where it
  doesn't stand.
- The command's glue is `if !plan.CheckNeeded`; `WhereRequest.Explicit()`
  is the engine's own `explicit()`.
- Call sites switched: `prepareSubmit`.
- Test: the tests-asked loop in `TestEachEnvironmentTakesItsNewestCheck`.
