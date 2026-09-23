# 2026-09-23: the review's fixes, steps one to four

The [review](../reviews/2026-09-23-architecture-review.md)'s order, as
the [reconciliation](2026-09-23-review-reconciliation.md) accepted it,
through its fourth step: six commits from `cc8ee174`, each gated on the
full suite, `make deadcode` clean after each. The two design-sized steps
that remain, the contracts and the request values with the command
pipeline, are reported on separately before they start.

## Defects, `cc8ee174`

- `macports.PortInfo.Bool(option)` reads a Tcl boolean one way, case
  folded, `on` and `off` among the words, and refuses anything else;
  the seven sites read through it. A port that says `use_xcode on` now
  records `NeedsXcode` true in its scope.
- GitHub verification locks a fork branch under the forge's name, the
  constant publication already used, not a provider name that happened
  to spell the same.
- The job log downloads through `fetch.Open` on the provider's own
  client, with dockhand's user agent and a 64 MiB bound, in place of the
  default client and no bound.

## Deletions, `1f0f271a` and `376f0c6c`

- The `review` stub and everything only it reached: the `cli` command
  file, `execute`, the "planned" group, the two review control kinds,
  the change and revision selectors on a control request, and the
  workflow command in `runtime` that dead-code analysis found once the
  stub was gone.
- `state.Query`'s `Newest`, `WithBuild`, and `Action`, with their SQL;
  `PreparationRequest.Selection`; `Services.Preparation`; the
  verification target's `Prerequisites` and `Inputs` and the artifact
  requirement type; `NativeReader`; `PlanSingle`. Test-only exports moved
  to test files: `cli.NewRoot`, the evaluator's wrappers, the syntax
  spans. The two aliases replaced by their targets.
- The archive fallback in `applyArchivePlan` is an explicit error,
  `portedit: archive plan without observed contexts`, gated on the suite
  proving nothing reaches it; with it went `Store.Refresh`,
  `CheckChecksumSources`, `portfile.ReplaceChecksums` and its keeping
  variant, the checksum group type, and `checksumKind`.
  `fidelity.Checksums` stays: the review counted it unreachable, and
  `ScopedChecksums` uses it.
- `guestShell` spells the guest directory once in `verify/tart`; five
  names with no user outside their package are unexported.

## Leaves, `a4982190`

`macports/commitmsg` holds the commit message rules and
`macports/fetchguard` the pre-fetch hook grammar with its effect table;
the local build options and the two unavailability errors live in
`verify`. `publish`'s closure is twelve internal packages, from 28, and
`workflow/choice` imports no provider. The evaluator is 700 production
lines; the grammar is 916 beside it with one call site.

## Single rules

Two commits, the record-level rules and then the vocabularies.

- **The preparing actions.** `record.Action.Updates` sits beside
  `Prepares`: the three actions that prepare an update from master,
  whose selection can resolve fresh, continued, or adopted. The initial
  phase table, the verification plan, the resolution, the preview, and
  the CLI ask the record; the scheduling SQL spells the preparing set
  once as `preparingActions`, and a test holds that spelling to the
  method for every action. The CLI's check that its three update
  commands were update commands is gone.
- **The job's next phase.** `record.JobPhase.Next(spec)` says which
  phase follows and when none does; `Valid` says what a phase is. The
  store refuses any other step, and the three workflow transitions take
  the phase it names. The store's rule got tighter in two corners the
  workflow never entered: a branch-ready job cannot step at all, and a
  job whose destination is not a pull request cannot step to
  publication.
- **The cancel shape.** `record.ControlRequest.ValidCancel` is the kind,
  the ID, a reason that is text, and no applied time. Intake and storage
  both call it and each still checks the side it owns: intake wants the
  submission time unset and the jobs as named or as selected, storage
  wants both set. Storage now refuses a reason that is not UTF-8, which
  it silently rewrote before.
- **The master fetch.** `Engine.FetchMaster` is exported and adoption
  calls it; `app.preparationSource`, the same six lines, is gone.
- **The checksum vocabulary.** `portfile.Checksum.Value(kind)` is the
  one switch over the five algorithm names, and `IsChecksumKind` asks
  it; `distfiles.algorithm` is gone. `archives.Download` embeds
  `portfile.Checksum` instead of repeating it, so `ChecksumValues` is
  gone and the refresh reads values off the download. A download's JSON
  carries the same keys, checksum first.
- **The guard grammar's control structures.** `switchReason` and
  `loopReason` take their shape from `syntax.Command.Control`, as the
  Tcl syntax consolidation said everything would; what the guard adds is
  its own, which of a foreach's selecting words are variables and a
  catch's result variables, which run nothing. The switch reader now
  reads bare arms as well as the braced list and judges the patterns as
  arguments; `Control` itself now says a switch with no arms, or a
  pattern with no body, is no control structure, which is the error Tcl
  makes of it.
- **One digest.** `record.Digest` is the hex SHA-256; the copies in
  `tart`, `verify/tart`, `verify/github`, and `portindex` are gone.

## Left in place from step two, and why

- `Engine.Provider`: `verificationProvider` falls back to it, and 22
  test literals set it. It goes with the verification ledger contract.
- `scratch` on `syscall.Flock`: `TryExisting`'s grace for a forked
  process is not what `filelock` offers; a fold would change behavior.
- `Reader.Revisions` and `SubmissionsForAttempt`: six tests read
  through them. They go when the tests that need them are rewritten
  against the projection.
- The `ControlBranch`, `BranchScope`, `PreparationInput`, and
  `CheckContinuation` exports, the `workflow/preparation` re-exports,
  `macports.NewTree` and `Projected`, and the `tcl/shell` and `outdated`
  folds: each is a small surface change with a caller to move first,
  queued behind the contracts rather than done piecemeal.
