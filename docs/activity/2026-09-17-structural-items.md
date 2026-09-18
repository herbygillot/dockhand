# Four structural items and the coverage intent

The [architecture review](../reviews/2026-09-17-bump-workflow-architecture.md)'s four structural items, taken as one queue item with the coverage-intent change folded in. Each removes a hazard that was hit during the day's work; none was large.

## An explicit prepared result

Four sites read `result.Fidelity[len(result.Fidelity)-1].After` for the evaluated snapshot of the prepared files: the go.mod toolchain check, dependency regeneration, the patch check, and the preparation adapter's final evaluation of the committed candidate tree. The list position was the contract, and a step that appended a report without meaning to change what the next step read would have changed it. `portedit.Result` now carries `Prepared`, the evaluated snapshot, set by `report` whenever a fidelity report is recorded; readers read that. The adapter's result carries it too, bound to the committed source identity once the candidate tree is written, with the last report keeping the same snapshot as evidence. Neither is emitted in JSON.

## One stub resolution

A stub selection such as `py-foo` was redirected to its newest versioned subport in binding, again in the editor's `load`, and a third time when the job's request cleared the subport so the editor would redirect the same way. `macports.ResolveStub` is the one place that decides: binding calls it and records the carrier target and the stub's name on the job; the editor honors a recorded stub, refusing one that does not name the selected subport's stub, and resolves only a selection nothing has resolved yet, which is the preview and assessment path. The job's request selects the carrier directly.

## One edit intent

`--keep-old-checksums` was threaded as a field of `app.Preparation`, `app.PreviewRequest`, `workflow.PreparationRequest`, `record.PreparationSpec`, and `portedit.Request`, beside `SharedRelease` and the stub name, each copied by hand at every step. `record.EditIntent` holds the three: the person's choices for the edit, set once from the command line and carried as one value through the job record to the editor, embedded in each type on the way so existing readers and the stored JSON are unchanged. The editor's `CommitName` is now the intent's `Stub`.

## The view in its own package

The contribution view, `Project` and the words it chooses for phases, states, and next steps, imported nothing from the engine or the store but lived beside them. It is `internal/workflow/view` now, with the snapshot it projects (`Snapshot`, `JobStatus`) defined there; `workflow.Status` is the engine's read of a repository, its filter and time around an embedded snapshot, and `Overview` pairs it with the projection for the status command. The tests moved with it. Nothing operational depends on how a row is worded.

## Coverage as an intent

`ReleaseScope.RequiredTargets` took a whole job spec and, when the spec's initiating target was not among the buildable members, fell through to every buildable member: the only path that yields the still-unexplained `py-idna` refusal, "missing shared-release target py310-idna". It now takes an explicit `record.CoverageIntent`, the initiating target alone or every buildable member, with `JobSpec.Coverage` deriving the intent from `AllSubports`, and an initiating target that builds nothing in the release is an error at every caller: dependent discovery, verification planning, the single-target contract, and publication coverage. A stub as the initiating target is refused rather than widened.

## Also

The GitHub log-retention test's identity assertion flaked again under the full suite's load and passed alone three times; `PruneLogCache` now checks the recorded run's identity before taking the cache lock, so a wrong run or repository is refused whether or not the cache is busy or gone, which is the assertion that failed.

## Tests

`record`: the coverage intent's three outcomes, the unknown intent, and the spec-derived intent. `fidelity`, `portedit`, `preparation`, `workflow`, `view`, `cli`, `tui`, `proc`, `app`, and `sqlite` pass unchanged in what they assert; the whole suite passes and `make deadcode` is clean.
