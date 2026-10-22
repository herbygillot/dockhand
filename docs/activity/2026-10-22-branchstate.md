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

## Step 2: attention and Next on the check's standing

status's `attentionFor` judged and worded every row in one function of the command, with `pullRequestAttention` and `submitNext` beside it. It's now `branchstate.Attend(facts)`. That returns at most one typed `Attention`:
- its `Kind` (failed, needs_you, or ready);
- one of 22 `Reason`s;
- the data its words need;
- a typed `Next`.

`Attention.Words(vocabulary)` holds each reason's sentence. The nouns are worded by their owners through `branchstate.Vocabulary`, which `engine.Vocabulary` implements with `DescribeEnvironment`, `Describe`, `Ago`, `RemadeWords`, and `HoldLines`, so branchstate imports none of them. `BranchStatus.Facts()` gathers the facts.

The command is now a call plus rendering:
- `attentionFor` maps each row's kind, words, and `renderNext(next)`. `renderNext` is the one place a Next becomes a command line. Every status test passes on it, its lines unchanged.
- `TestEveryNextVerbRenders` holds every verb in `branchstate.Verbs` to rendering as one.

Attend reads the check's standing (`Check.Standing`) where status re-derived it from the runs beside it. That settles the two disagreements the map found, which `TestAttentionReadsTheChecksStanding` pins:
- a baseline running beside a passed check hid status's row; it no longer does;
- a failed extra (from --also, or a revision-only port) read "passed; waiting for you to submit" while submit refused it. It now asks to be accepted, "submit --accept <port>".

## Step 3: serve's passing branches from the one definition

`PassingBranches` defined a branch that passed and is ready to submit inline, for serve and for `ship --passing`. That definition is now `branchstate.SubmitReadiness(facts)`, which says a branch is not a candidate, ready, or not ready. `PassingBranches` and, through it, `serveCandidates` call it. `Branch.Commits` joins the facts.

`Evaluate(facts, ActionSubmit, mode)` refuses a branch that isn't ready, and its Next follows from the record and the check's standing:
- adopt, for a branch gone;
- status, for one already pushed;
- tidy, for nothing committed or files edited;
- wait, after the check finishes, for one checking;
- wait, for one stopped;
- logs, for one failed;
- check, otherwise.

The contract table now holds these eight refusals, and every one's Next advances. Submit's own blocks are step 4.

## Step 4: submit's blocks are typed, and the publication rule is branchstate's

submit appended a sentence at each of its blocking sites. Each now records a `branchstate.Block` of one of 15 kinds, through `SubmitPlan.block`:
- unconfirmed guesses, a merge commit;
- a check pending, a check of other files, no check;
- an unmet target, a remade target, an unchecked target;
- a failure the base shares, a failure, a failure not accepted;
- someone else's push, maintainers not allowed to edit, no write access;
- no title.

The block's words are `Block.Words`, the sentences submit said, word for word, and `Blocking` keeps them. The data each sentence names rides on the block, and the nouns go through the vocabulary, which gains `Unmet`.

The publication rule is branchstate's, with the acceptances it reads, moved from engine/evidence.go:
- `PublicationBlocks`, where `publicationProblems` built sentences;
- `AcceptanceProblem`, `AcceptedReasons`, and `BlockedByAccepted`;
- `EnvironmentFailures` and `StandingAccepted`;
- `AcceptedFailure` with its kinds and words.

The engine keeps aliases for `AcceptedFailure` and its kinds, and its `StandingAccepted` method reads the store and calls branchstate. The engine's tests read the rule through test-side wrappers that word the blocks as submit does. Every test passes unchanged, which shows the words didn't move.

`submit --json` gains `blocks`, each with its kind, its port, and its words. Still to come: the remaining sites' conditions (a merge commit, a missing title) are simple reads of facts the engine gathers as it plans. They move to an `Evaluate(submit)` over gathered facts with step 5, which settles one gate for ship and serve.
