# Review: structural pass

Date: 2026-09-19. Reviewed the clean working tree at `65518c3`. This is a
whole-tree organization pass rather than a focused follow-up: the question asked
was whether fast change has left the package structure lopsided. It has not, in
the large. The shape that [components.md](../components.md) describes is the
shape the code has, and the two places where it has drifted are both cases of a
migration that was started and not finished. These observations are proposals
for triage, not accepted implementation work.

## What the measurements say

59 packages, 40,650 non-vendor non-test lines. `go vet` is clean, `make deadcode`
reports nothing, the suite passes, and cross-package statement coverage is 81.3%.
The declared dependency rules hold: `record`, `text`, `filelock`, and `atomicfile`
import nothing internal; `state` imports only `record`; `verify` imports only
`record` and `git`; `macos` imports only `record`. Nothing outside `app`, `cli`,
`proc`, `tui`, and `tools/stateperf` imports `workflow`, so no capability depends
back on the engine. `verify/github` and `verify/tart` reach `state` only through
`ProviderStore`, `ProviderReader`, and `ProviderTx`, as designed. `cli` touches
`state` for one error type and one default path.

The two largest packages were the ones most worth suspecting, and both survive
inspection. Measuring file-to-file symbol references inside `workflow` gives a
hub-and-spoke shape: the phase families (`verification_*`, `publication_*`,
`preparation_*`, `contribution_*`, `retention*`, `control*`) make roughly 150
references into the shared spine of `engine.go`, `cycle.go`, and the intake files,
and roughly 33 to each other. No file exceeds 365 lines. That is the structure
[components.md](../components.md) argues for — "scheduling, claims, transitions,
retries, and recovery stay together because they jointly determine whether work
may advance" — and it is holding rather than eroding. `macports/portedit` shows
the same shape, with every edit kind converging on `prepare` and `source`.

Size alone is therefore not the problem. `workflow` is 16% of the tree because
it owns one genuinely large responsibility, not because it is a bucket.

## 1. The status projection replaced one vocabulary and left the other standing

This is the real drift, and it is user-visible.

[output.md](../output.md) agreed on 2026-09-17 that the engine gains "one
projection over its existing reader, used by the plain output, the JSON result,
and the table alike, so all three say the same thing", and that "the 'next'
derivation moves out of the CLI's progress formatting and beside this projection."
[`workflow/view`](../../internal/workflow/view/contribution.go) was built and
`status` was moved onto it. The CLI's own derivation was never retired.

Action commands take a different path. `dockhand bump`, `verify`, and `publish`
render through [`renderSummary`](../../internal/cli/summary.go), which derives its
own words in `jobHeadline`, `jobState`, `versionMove`, `pullRequestLine`, and
`pendingGuidance`. `dockhand status` renders through
[`renderContributions`](../../internal/cli/status.go), which consumes
`view.Contribution` fields. Two vocabularies now describe the same records, and
they have already diverged:

- An active verification job with attempts queued for capacity is
  "waiting for capacity" to `status` ([`activeState`](../../internal/workflow/view/contribution.go)),
  and plainly "verifying" to `verify` ([`jobState`](../../internal/cli/summary.go)).
  A running attempt is "building on <platform>" to one and "verifying" to the other.
- A preparation job past its edit is "integrating branch" to `status` and
  "preparing" to `bump`.
- `jobPort` exists in both packages with the same name, the same parameter, and
  different behavior: the CLI copy appends sorted variant suffixes and falls back
  to the bare job ID, the view copy drops variants and falls back to
  `"--job " + ID`.

The divergence is not a bug to patch in place; it is the predictable result of
leaving the second derivation alive. Retiring the CLI's word-derivation onto
`view` — leaving `summary.go` responsible for layout, ordering, and the
identifier-level detail the projection deliberately omits — finishes the
migration output.md described and removes the duplicate `jobPort` by
construction.

Note also that [components.md](../components.md) still describes `view/` as
"records only". It has not been records-only since it was created; it is the
phrasebook, by the later decision in output.md. Whichever way this is settled,
the two documents should not keep disagreeing.

## 2. The TUI owns command-tree knowledge it cannot be checked against

[`verbArgs`](../../internal/tui/status.go) builds argument vectors — `"--change"`,
`"--job"`, `"--detach"`, and the verb names — and a switch in `verb` decides which
verbs need confirmation. `internal/cli` imports `internal/tui`, so `tui` cannot
import `cli`, and these spellings are strings on both sides with nothing tying
them together.

The authority model here is right and worth preserving: `Options.Run` re-enters
the command tree in-process, so a keypress has exactly the authority of the
command, as [`liveStatus`](../../internal/cli/status.go) documents. The weakness
is only that renaming a flag in `cli` breaks the table silently, with no compile
error and no test that would notice.

Moving `verbArgs` and the confirm set into `cli` and passing them through
`Options` as another injected callback would leave `tui` responsible for
rendering and key handling alone, and would put the flag spellings back in the
package that defines them.

## 3. Conventions that stopped being applied to new packages

Two mechanical drifts, both dating from work after the conventions were set.

The [package documentation pass](../activity/2026-09-15-package-documentation.md)
established "one overview in `doc.go`; earlier package comments were moved out of
implementation" across the then-44 packages. All eight packages created since
— `archive`, `macports/fidelity`, `macports/patchcheck`, `macports/version`,
`subprocess`, `tui`, `version`, and `workflow/view` — carry a correct package
comment in their implementation file instead. Nothing is undocumented; the
convention simply stopped being applied on 2026-09-16.

The package map block in [components.md](../components.md) has not kept up with
the tree. `macports/dependency`, `macports/distfiles`, and `tui` are described in
the prose but absent from the map; `internal/version` appears in neither. The map
is the first thing a reader consults, and it is now the least current part of the
document.

## 4. Smaller observations, not obviously worth acting on

`internal/github` is aliased `githubapi` at 21 import sites, the most-aliased
package in the tree, because `forge/github` and `verify/github` compete for the
name. The three-package split itself is sound and the layering is clean —
`internal/github` is a leaf owning auth and transport, and the other two are
independent consumers. A name matching its role would remove the aliases; this is
cosmetic and can wait for a reason to touch those files anyway.

`macports/distfiles` has exactly one consumer, `macports/portedit`. That is a
defensible boundary, since it is separately testable and the workspace package is
already the largest under `macports`, but it is the one package in the tree whose
existence rests on testability alone.

`workflow/policy` has no test files of its own. Its effective coverage is 76.2%
through `workflow`'s tests, so this is not a hole, but it is the package whose
entire reason for existing is that judgment should be "testable without running a
VM or contacting GitHub", and it is currently only tested through the engine.
`internal/text` is in the same position at 72.5% with no direct tests.

## Suggested order

Item 1 is the only one with user-visible consequences and is the natural
completion of work already agreed. Item 3 is mechanical and could ride along with
anything. Item 2 is small and prevents a class of silent breakage. Item 4 needs a
decision only if someone wants one.
