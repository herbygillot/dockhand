# One verdict on a branch's state: branchstate (v0.5.0 part 4, item 1)

## The shape, settled with architecture

Mapping the six judgments found them in these places:
- status's `attentionFor`;
- serve's `PassingBranches` and `serveCandidates`;
- submit's ten or so `Blocking` sites;
- tidy's `Keep`, `Unambiguous`, `AppliesUnasked`, and `HeldForLook`;
- `Held` and `Concerns`;
- ship's gates. `goOn` is now engine/ship.go's `commitUnasked`.

Architecture's answer settles how they become one:
- **The package.** `internal/branchstate` is pure, over model and evidence. The engine gathers the `Facts`. `Evaluate(facts, action, mode)` gives one `Verdict`. The migration takes five commits.
- **`Next` is the verdict's, and typed.** It holds a verb, the branch, its arguments, and `After`: what must change before the same action can be asked again (a person's look, a check finishing, a person's edit). The contract test beside `Evaluate` says a refusal's Next names another verb or an After, never the same action asked again with nothing to wait for. The command renders a Next as a command line, so the help-reference test proves every rendered verb and flag exists.
- **Each kind's words go with the kind, in branchstate,** since serve, in the engine, says them too. The command adds layout only.
- **Facts that a read can fail or leave stale say unknown apart from false:** the pull request observation, the fork and push permission, a Git-fetched target's tag. Unknown never equals: serve-submit holds on it, and status says it's stale.
- **Mode is `Answered` alone** for consent. Origin is a fact, never a second reading of consent.
- **Done when** command/ship.go makes no judgment, and `attentionFor`, `submitNext`, `checkWords`, the Blocking sites, and `PassingBranches` are each a call plus rendering.

## Step 1: the check's standing, and the Next contract

`CheckStanding` and the active-check predicate (R3-2) move to `branchstate.Check.Standing` and `Check.Checking`. That's the one definition of the branch's checks, apart from baselines and runs asked to stop. `BranchStatus.Check()` gathers the facts, and its `CheckStanding()` and `Checking()` delegate to it. The command renders `branchstate.Standing` and imports branchstate, named in its boundary test. branchstate's own boundary test allows model and evidence alone.

`Verdict`, `Facts`, `Action`, `Mode` (`Answered`: asked, yes, or nobody), and the typed `Next` with `After` land with `Evaluate`, which says the standing so far. So does the contract test `TestARefusalsNextAdvances`, over a table of cases that's empty now; each step adds the refusals it brings.
