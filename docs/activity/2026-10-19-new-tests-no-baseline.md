# No baseline for tests master doesn't run

The roadmap's v0.5.0 part 1 item 5, its fourth line, from the exiv2 field
notes: exiv2's check offered `check --baseline` for tests the branch
declares and master doesn't, where a baseline can't say whether master
fails tests it never runs.

- `Engine.testsNewHere` sets apart, of the ports a baseline would rebuild,
  each whose only failures are its tests' (`onlyTestsFailed`: every
  failure while it built is at the test phase, or a pass with its tests
  failed) and whose change record says `dockhand.test_run` moved from
  false to true (`testsDeclaredHere`). A port with no record is kept.
- `BaselineCandidates` returns a `BaselineOffer`: the ports, the base, and
  `NewTests`. The check's report says "exiv2's tests are new on this
  branch, and master declares none, so a baseline has nothing to compare."
  in place of offering one, and `check.baseline` runs none for them.
- `PlanBaseline` without `--only` leaves them out too (`Baseline.NewTests`),
  and refuses with the same words where nothing else is left; `--only`
  still builds them.
- `macports.ParseBool` reads a value as MacPorts' tbool does, which
  `PortInfo.Bool` now reads through, for the record's From and To.
- Test: `TestTestsNewOnTheBranchAreNoBaselines`, and the offer in
  `TestOnlyTheNewestFailedCheckPointsToABaseline`.
