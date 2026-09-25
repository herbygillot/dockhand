# 2026-09-25: the command layer decides nothing

Asked whether each command should get a sub-package of `internal/command`,
to avoid the pile-up v2's `cli` had, the person accepted the answer that
packaging wasn't v2's problem and wouldn't prevent the next one.

## Why not a package per command

The [2026-09-21 review](../reviews/2026-09-21-architecture-and-organization.md)
measured v2's `cli` at about 3,700 lines. It found:

- one sequence repeated thirteen times;
- a fan-out of 20, reaching into domain packages to work out exit codes
  and hints rather than asking the result;
- evidence rendered three times.

All of that is logic in the wrong layer, which a package boundary would
move without removing.

v3's `command` was 6,746 lines in 28 files, one per verb, with the v2
repetition already factored (two `engine.Open` sites, shared `s.open` and
`chooseBranch`) and a fan-out mostly to `engine` and `model`. Sub-packages
would have needed an exported package of shared helpers (streams,
settings, prompts, the JSON envelope), which is where a new pile starts.

## What had leaked, and where it went

- **Two definitions of a passing branch.** `submit --passing` decided in
  the command which branches passed; serve had its own test in the
  engine. Both read `engine.PassingBranches` now, and serve adds only
  that it opens pull requests for the branches it started.
  `TestPassingIsOneDefinitionForSubmitAndServe` edits a passed branch and
  sees it leave both.
- **The linked-port revbump.** `update --revbump-dependents` looped in the
  command, choosing the subject and what a failure partway leaves. It is
  `engine.RevbumpLinked` (`TestRevbumpLinkedBumpsEachLinkedPortForTidy`).
- **`--on`.** Which providers a check builds on was decided in the command,
  where `--on tart:<releases>` would have extended it. It is
  `engine.Environments` (`TestEnvironmentsAreTheProvidersOnNames`).
- **Store reads.** The command read plans and the journal with the store's
  own transactions, in four places. The engine offers `Plan` and
  `Events`, and exports `ErrUnsupported`, so the command no longer matches
  preparation's error.
- **serve.** Its loop, leadership, pull-request following, cleanup, daily
  look for new releases, and passing submitter were in the command. They
  are `engine.Serve` (`internal/engine/serve.go`). `serve.submit_limit`
  was counted in a `submitted.json` beside the database. It now counts the
  journal's `serve.submit` events since midnight, through a new
  `store.Reader.CountEvents`, so it holds across restarts from the
  database itself. The daily-limit test seeds the journal, and fails when
  only yesterday's submission is there. The command keeps the flags, the
  configuration, launchd, and notifications.

`command` is 6,185 lines now.

## The rule, and the test that holds it

`internal/command/doc.go` states the rule. A command parses, finds its
context, asks what only a person can answer, and renders; the engine
decides. Anything that chooses what is eligible, enforces a guardrail,
runs several engine operations as one, or keeps state belongs in the
engine.

`TestCommandTalksToTheEngine` enforces two things:

- **Imports.** The package imports only the packages named in
  `commandImports`, each with its reason: the engine, the records, the
  configuration, its own session, and the composition of providers and
  GitHub's login.
- **Store access.** No file calls the store's `View` or `Update`.

A throwaway file importing `preparation` and reading the store failed the
test with both named.

## Left

- **Vocabulary imports.** `record`, `macports/portfile`, and
  `macports/commitrules` stay allowed as vocabulary the engine's results
  carry, until the roadmap's vocabulary package exists.
- **serve's small files.** Serve's stamps and its `serving.json` and
  `outdated.json` are still files beside the database, owned by the engine
  now. Design v3 §11 makes the database the only coordination medium; the
  stamps could be journal events, and what the leader says about itself
  part of its session.
