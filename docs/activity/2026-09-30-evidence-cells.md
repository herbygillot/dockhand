# 2026-09-30: a cell of the evidence says what it is, and an extra follows one rule

This finishes item 6's fifth piece with the code-organization review's finding 25. The evidence's cells were bare `TargetResult`s. So an excluded cell was a made-up "not run", an unmet one carried no reason, and a remade one was noted in a side list. Every reader asked the plan again to tell them apart: the runner, the words, the JSON, and the publication rule. And three readers each spelled their own exemption for an `--also` extra.

## The cell

`engine.Cell` is a result with its kind, its environment, and, for an unmet cell, why:
- `recorded`: a result an execution recorded, or reused;
- `excluded`: the plan leaves the target out there;
- `unmet`: the environment can't build it, as the plan that found it says;
- `not-run`: required there, and no check of the files built it;
- `remade`: built there before the environment was made again.

`runEvidence` sets the kind, and `dropRemade` and `fill` change it. The words, the runner's settling, the JSON's `excluded` and `remade`, and the publication rule all read it, and `engine.Excluded` is gone. `TargetEvidence.Remade` is now derived from the cells, so `settle` no longer reconciles a side list.

The cell carrying its own reason fixes a gap. A changed target `--only` left out, filled from an earlier check where the environment couldn't build it, was unmet by that check's plan. The newer plan, which doesn't build it, was asked why and couldn't say. So submit said "no check of these files built it" rather than what the environment lacks, and the grid lost its words. Now both come from the cell.

## One rule for an extra

`TargetEvidence.Extra`, `Missing`, and `Failing` are the rule: an extra is built for what it shows, so it asks nothing where no check built it, and where it fails, its failure is accepted, never fixed. Before, the readers disagreed:
- status and the publication rule exempted an unbuilt extra from "no check built it";
- `Evidence.Failed` counted it as failed, so serve's passing branches left the branch out, and submit fell through to "did not pass; acknowledge it with --accept";
- `--accept` of such an extra was taken, with nothing to accept.

Now `Evidence.Failed` and `Evidence.Missing` follow the rule, so status, submit, `submit --passing`, and serve agree. `--accept` of an unbuilt extra says there's no failure to accept. A failed extra still needs `--accept`, and still keeps a branch out of serve, which can't accept for a person.

## Not taken

The finding's remedy also asked for the tree and changed paths on `BranchStatus`, so that `Diff`, `Impact`, `ArchiveDiff`, and `LinkedPorts` needn't read them again. That's a saving, not a correctness fix, and it isn't in item 6's order. It stays with the finding, to take when those verbs are next touched.

## Tests

- `TestACellFilledFromAnEarlierCheckKeepsWhatItIs` covers the unmet gap above: the words and the publication problem.
- `TestAnExtraFollowsOneRule` covers an unbuilt, remade, and failed extra, and a changed target beside them.
- `TestARemadeCellIsMissing` covers a remade cell filled by an earlier check made as the environment is now, and a not-run checkpoint read as no result.
- `TestSubmitFollowsThePublicationRule` adds an extra no check reached: nothing blocks, and nothing is accepted.
- `TestTargetWordsAreDesignV3s` covers excluded and unmet cells by kind.

Tests that built evidence by hand now build cells, through a `cells` helper in each package. Nine mutations each fail a test. One more, the kind check in `settle`, was equivalent, since a cell with no result never passes, and it was removed.
