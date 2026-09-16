# PortIndex follow-ups: test cache isolation and discovery batching

Two follow-ups recorded by the [consolidation report](2026-09-16-portindex-consolidation.md).

## Test cache isolation

`app.Config.IndexCacheDirectory` overrides the shared PortIndex cache root; when empty, `DOCKHAND_INDEX_CACHE` is consulted before the user cache directory. `TestMain` in the `cli`, `app`, and `workflow` test packages points the variable at a temporary directory for the whole package run and removes it afterwards, and the `gc` test that seeded a legacy entry under the real user cache now seeds it under a per-test directory. Running `go test ./...` no longer changes the generation count under `~/Library/Caches/dockhand/indexes`. Fixture generations written by earlier runs remain there until `gc` collects them.

## Where the warm `outdated` time went

A warm `outdated deno` took 87 seconds with the index cached. Timestamps on the progress lines placed 80 of them between "Probing editable version inputs" and the result: release discovery. Deno publishes 389 releases; every eligible one was evaluated through `VersionProbe.EvaluateVersion`, which rewrites the Portfile and starts a fresh `port-tclsh`, loads MacPorts, initializes the evaluator, and opens the port. The evaluation is required by design, since MacPorts computes the version from the tag, but the interpreter startup per candidate was not.

## Change

`eval.Evaluator.Open` returns a `Session` bound to one tree; `Session.Evaluate` and `EvaluateSelected` run the existing evaluation inside that interpreter, and each call still opens the port afresh so rewritten contents are observed. `Session` refuses a context from another tree root and satisfies the new `macports.BatchReader`/`macports.Batch` contracts, which `selection.Reader` inherits.

`portedit.evaluateContents` and `evaluateVersion` take the reader to evaluate with. `VersionProbe.EvaluateVersions` opens one batch for the workspace tree and evaluates every candidate through it, returning versions in order and naming a failing value; single values and readers without batching use the existing path.

`upstream.Service.EvaluateVersions` is set by `Bind` when the probe offers batching, without mutating the shared service. Automatic selection collects the candidates that need evaluation and evaluates them in one call, falling back to per-candidate evaluation otherwise. Explicit resolution and livecheck listings are unchanged.

## Checks

- New tests: a session observes rewritten metadata without restarting and refuses foreign trees; batched probe results equal sequential results and restore the Portfile; bound discovery uses one batch call and falls back to single evaluation for probes without it.
- `go test ./... -count=1` and `go vet ./...`: passed. The real user cache gained no generation from the suite; the one new entry during the run was the master tree staged by a manual `bump --diff`, and rerunning the index-staging CLI tests afterwards left the count unchanged.

## Measurements

Real tree, warm index, MacPorts 2.12.6:

| Command | Before | After |
| --- | --- | --- |
| `outdated deno` | 87s | 27s |
| `bump deno --diff` (automatic selection, master fetch included) | 95s | 42s |

Deno remains a heavy case because of its release count; the remaining time is dominated by 389 port evaluations at roughly 50ms each plus the release listing. Parallel sessions would need private copies of the workspace because probing rewrites the Portfile in place; that is a possible next step if maintainer-wide `outdated` scans need it.
