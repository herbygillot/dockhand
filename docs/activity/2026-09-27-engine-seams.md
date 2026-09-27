# 2026-09-27: seams in the engine

Roadmap item 4. The [architecture and data-flow review](../reviews/2026-09-27-architecture-and-data-flow.md)'s finding 4: `engine` was becoming the replacement monolith, and all three providers imported it.

## The provider contract is a package of its own

- **Before.** `Provider`, `Job`, `JobTarget`, `Build`, `ErrInfrastructure`, and the optional capabilities were in `engine`, beside `Fork`, `Leftover`, and `CheckBranchPrefix`. The Tart, GitHub, and command providers imported `engine` for them, so the package that drives providers was also one they depended on.
- **Now.** They are `internal/provider`, which was only a directory:
  - `Provider`, `Job`, `Target` (was `JobTarget`), `Build`, and `ErrInfrastructure`;
  - the capabilities the engine looks for: `ReleaseProvider`, `Remedier`, `OwnTestsProvider`, and `LeftoverProvider`;
  - `Leftover` as a provider reports it, `Fork`, and `CheckBranchPrefix`.

  It imports only `model`. The providers import it instead of `engine`, and the engine and the command layer, which composes the providers, import it too.
- **`Leftover` is split.** A provider reports `provider.Leftover`, with its provider, reference, and words. The engine's `engine.Leftover` embeds it, and adds what clean decides: the check it was made for, why it stays, and whether it was removed.
- **Each provider asserts at compile time the capabilities it has.** The Tart provider is a `ReleaseProvider`, `Remedier`, and `LeftoverProvider`; the GitHub provider is an `OwnTestsProvider`. The engine finds capabilities by type assertion, so a signature that drifts fails quietly. While moving `Leftover`, the engine's own test provider stopped being a `LeftoverProvider` that way, and only its tests' expectations caught it.
- **A test holds the line.** `internal/provider`'s boundary test refuses a provider importing `engine`, `command`, `store`, or `coord`, each with why. The command layer's allowed imports gain `internal/provider`, for composition.
- **Names.** Engine code that called a local variable `provider` renames it (`builder`, `lister`, `remover`, `named`, `own`), since the package now has the name.

## What the engine may import

- **A boundary test** (`engine/boundary_test.go`), like the command layer's, lists every package beyond the standard library the engine may import, each with why. Anything unlisted fails, naming the file: name the import and why, or move what needs it out of the engine.
- **What it leaves out:** the command layer, which drives the engine; the providers, which it drives through `internal/provider` and the command layer composes; and Tart's host control, which only the Tart provider uses. None of these was imported; now none can be without saying why.
- **The list only shrinks by being kept.** An entry the engine no longer imports fails too, so when item 5 moves `record`'s live types to their owners, `internal/record` comes off.
- **Thirty entries.** That is the review's 29 local imports, and `internal/provider`.

## History is a package of its own

- **Before.** The transition mechanics of item 3 were in `engine/history.go`: the branch's lock, recording a checkpoint as prepared and settling it, recording a restore, reading an uncertain commit back, and finishing what a stopped change left.
- **Now.** They are `internal/history`, as `history.Transitions`: `With` (the lock, after finishing what was left), `Prepare`, `Settle`, `RecordRestore`, `Read`, `SetBase`, and `Step`, the test's stop point. It imports `git`, `model`, and `store`. The engine builds one (`e.history()`) from its repository, store, clock, and worktree opener. The verbs stay in the engine: what a tidy composes, what a rebase replays, and what a restore checks all decide the change, and the transition carries it out.
- **Its own tests** cover two rules the engine's tests, which drive the verbs, don't:
  - a change counts as made when the branch holds its new head with a person's commits on top;
  - a change whose branch is somewhere it never held is abandoned, with its refs removed.

  The engine's tests of stopped and uncertain changes stay where they are, since they stop a real tidy, rebase, and restore.

## The test-policy judge is a model rule

`engine.Judge` is `model.TestPolicy.Judge`: fifteen lines over model types, the rule a result is recorded under. A package of its own would hold one function. The runner calls it as before, and its test moved with it.

## What waits for item 6

Per-environment planning, and the rule for which results count (`engine.Counts`), were fixed in items 1 and 2 but stay in the engine for now. Item 6 changes both:
- the port reader returns an evaluation report, which is the planner's input;
- reuse keys results by what each build consumed, which replaces the rule.

Moving them now would move them twice. The roadmap says so under item 4, and they move with item 6.
