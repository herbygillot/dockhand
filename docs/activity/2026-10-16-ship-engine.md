# ship's engine (v0.4.0 part 2, item 3)

Built in slices, each committed on its own.

## The plan's readings, the unasked tidy, and the consent answer

- `model.Plan` gains what `check`'s plan text worked out while printing:
  `Untested()` (what a plan requiring tests asks nothing of, and what it
  does), `SameOrder()`, `GroupedExclusions()` with `Everywhere`, and
  `BuildCount()`, which `--variants each` asks above twelve of. `ship --to
  check` prints the same plan, and reads them rather than working them out
  again (the command package split's 7b row 9).
- `model.Answered`, moved from command, where the consent value lived: the
  engine reads it now.
- `TidyPlan.AppliesUnasked(answered, explicit)`: a plan made only of
  dockhand's own edits applies where nobody was there to ask, any plan the
  person answered for, with --yes or by giving it, and none while
  something blocks it. tidy's unasked path and serve's preparation read it,
  where each judged `Unambiguous` on its own (7b row 6); it's ship's
  "commit if unambiguous".
