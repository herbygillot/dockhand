# One active-check predicate, and the drifted help text

The first commit of the roadmap's v0.5.0 part 1 item 4, "One owner for
each rule": the architecture review of 5897a82's R3-2 and R3-27, in a
commit of their own so an rc6 can carry them.

## R3-2: the branch's active check is one predicate

`BranchStatus.Active` took every queued or running run, where `ActiveCheck`
and the queue dropped baselines and runs asked to stop. So while a baseline
of a passed check ran, `CheckStanding` said Checking and serve's
`PassingBranches` waited on the branch, though `ActiveCheck` said it had no
check.

- `model.Run.BranchCheck()` is the rule: not a baseline, not asked to stop.
- Call sites switched: `Engine.queue` (both the refusal's guard and the
  loop over other runs), `Engine.ActiveCheck`'s filter, and
  `BranchStatus.CheckStanding`, through the new `BranchStatus.Checking()`,
  which status's check column reads for the run it names.
- `Active` still lists every queued or running run, for status's JSON and
  for judging a stopped run.
- Grep for the old shape: `BaselineOf != "" || .*CancelRequested != nil`
  finds none left. The other `BaselineOf` reads are about finished runs
  (status's latest, a baseline's own run, check's output).
- Test: `TestABaselineOrAStoppingRunLeavesTheBranchsStanding`.

## R3-27: the drifted text

- `"queue"` was in root.go's `--json` list, a command gone since d997488.
  The list is gone: each of the 29 constructors marks itself with
  `supportsJSON(cmd)` before returning it (config's and explain's, which
  returned a literal, now name it `cmd`).
- outdated's help offered `update --outdated --mine`; it now says
  `update --mine` starts on them and `ship --mine --to check` checks them
  too, as the README does.
- The help-reference test missed it because it read only text naming
  "dockhand ". It now also reads a line of prose that starts with one of
  dockhand's commands and a flag (`bareCommand`), in help and in source
  literals; `TestReferenceProblems` covers the outdated case, and that
  `port -v` isn't read as one.
