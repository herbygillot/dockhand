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

## CaptureAuto and the active check

- `engine.CaptureAuto`, the files a command asking for none in particular
  means, which check's `captureMode` decided from three engine facts
  (`Current`, `Edited`, `CommitsAhead`): the working files of the branch
  checked out here, or of a named one with no commits of its own; a named
  branch's committed tip where its worktree has no edits; and a refusal
  where it has both. `CaptureRequest.Named` says a flag named the branch.
  check's flags now only pick a mode, or `CaptureAuto`.
- `engine.ActiveCheck`, the branch's one check queued or running, with
  `ActiveRunError.Stopped` judged by it, and `engine.Replace`, which stops
  the runs a new check replaces and returns each as it is after
  (`Enqueue(Replace)`, row 7). check's `replaceActive` keeps the question
  to a person before a running check is stopped, and the words; it judges
  with an observer session and stops with a foreground one, where it held a
  foreground session for both.

## The baseline comparison, and the auto-run without an exit code

- `engine.BaselineComparison(base, branch)` sets each port's result at the
  base beside the branch's, in each environment where the baseline built
  it, and where the check failed it but master doesn't build it: the
  judgment check's `writeBaselineResults` made while printing (review row
  6, moved here from 5.5 by the split's coalescing), so the `run` library
  holds none. The words stay command's.
- `check.baseline`'s auto-run asks the engine whether a baseline would
  answer anything (`BaselineCandidates`, of the finished run), where it
  first branched on the check's exit code being `ExitCheckFailed` (7b row
  8).
