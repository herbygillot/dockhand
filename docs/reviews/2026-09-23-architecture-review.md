# Architecture review — 2026-09-23

Reviewed commit: `9b00714`. The tree is 64 packages and 49,247 production
lines. Three read-only passes covered the MacPorts evaluation and editing
stack; workflow, state, app, and cli; and verification, publication,
forges, Git, and the utilities. A whole-program reachability pass
(`deadcode ./cmd/dockhand`) and cross-cutting checks of subprocess and
HTTP use were made alongside them, and each finding below was checked
against the code before it was written down. Items the 09-21 and 09-22
reviews raised and the tree has since fixed are not repeated.

The shape is sound. Contracts are explicit, and no package has a cycle.
The heavy packages have reasons for their weight: `workflow` (7,334 lines),
`cli` (3,754), `portedit` (3,095), `state/sqlite` (2,504), `app` (1,525
lines, 37 internal imports). What this pass found is not one large
problem. It is a set of small ones that share a pattern. A rule written
once gets a second copy when a new caller needs it. A small predicate
lives inside a large package, and every importer drags that package
along. A struct passes as a whole where a caller needs two of its
fields.

| package | lines | files | imports | imported by |
| --- | ---: | ---: | ---: | ---: |
| `workflow` | 7,334 | 47 | 15 | 5 |
| `cli` | 3,754 | 25 | 19 | 0 |
| `macports/portedit` | 3,095 | 21 | 17 | 6 |
| `state/sqlite` | 2,504 | 14 | 3 | 1 |
| `macports/portindex` | 1,934 | 9 | 9 | 8 |
| `git` | 1,864 | 12 | 3 | 21 |
| `verify/tart` | 1,720 | 15 | 16 | 2 |
| `macports/eval` | 1,635 | 8 | 7 | 2 |
| `record` | 1,619 | 17 | 0 | 35 |
| `app` | 1,525 | 21 | 37 | 1 |

## 1. Overlapping concerns

**One rule, several copies.** These have already drifted, or will at the
next change:

- *Tcl booleans.* `use_xcode` is read three ways. `Snapshot.RequiresXcode`
  (`macports/metadata.go:61`) ignores case, knows `on`/`off`, and rejects
  anything else. `RebindReleaseScope` (`macports/scope.go:34`) and
  `fidelity.ReleaseScope` (`fidelity.go:266`) accept only `yes`, `true`,
  and `1`. A port that says `use_xcode on` is therefore recorded with
  `NeedsXcode` false in its release scope. `extract.rename` is also read
  two opposite ways: `portedit/patches.go:66` and
  `dependency_source.go:64` treat only yes-words as true, while
  `distfiles/manifest.go:47` treats everything except no-words as true.
  One `macports.PortInfo.Bool(option) (bool, error)` settles all seven
  sites.
- *Preparing actions.* The set is written in `record.Action.Prepares()`,
  `workflow/phase.go:9`, `verify/plan.go:66`, and the SQL at
  `state/sqlite/records.go:495`. The three update actions appear again in
  `workflow/resolution.go` (`updates`), `app/preparation.go`, and
  `cli/preparation.go`.
- *Job transitions.* Cancel validation is the same predicate in
  `workflow/control.go` (twice) and `state/sqlite/control.go`. Phase order
  is `jobPhaseOrder` in SQLite, with its skip-verification exception, and
  is assigned in five places in `workflow`. SQLite should keep checking,
  but both sides should call one `record.JobPhase.Next(spec)` and one
  `ControlRequest.ValidCancel()`.
- *Master fetch.* `app.preparationSource` is `workflow.Engine.fetchMaster`
  word for word, and adoption still calls the copy.
- *Provider policy.* The verification provider and test policy are decided
  in `cli/build.go` (auto becomes Tart when a Tart flag is set, tests
  default, `--tests` refused with GitHub). They are decided again in
  `app/verification.go` (GitHub refuses `--fresh`, the same refusal as
  `cli/actions.go:87`) and a third time in `workflow/choice`, which was
  meant to own them.
- *Checksum vocabulary.* `portfile.checksumKind` and `distfiles.algorithm`
  are one function. The per-algorithm value map is built in
  `portfile/checksums.go` and again in `portedit/artifact_apply.go`.
  `archives.Download` repeats `portfile.Checksum` field for field behind a
  converter.
- *Tcl control structure.* `eval/effect.go`'s `switchReason` and
  `loopReason` re-parse options, arms, and loop words that
  `syntax.Command.Control` already returns. The Tcl syntax consolidation
  of 2026-09-20 said nothing outside `tcl/syntax` would do that again.

**Two implementations of one job:**

- *The two verification providers each keep a copy of the provider
  execution ledger.* Both register a pool, lock per request, read and
  write executions with the same repository-scope conflict check, and
  close unknown requests during reconciliation. See `verify/tart/execution.go`
  and `lifecycle.go` against `verify/github/provider.go:56-86, 329-336`.
  Log paging is duplicated too. The rule that a closed request refuses a
  later submit is the one this ledger exists to keep, and it lives in
  both copies.
- *Image readiness is checked twice.* `tart/provision`'s `Validate` and
  `verify/tart`'s `InspectCapabilities` run the same guest probes: sudo,
  foreign package managers, the MacPorts installation, active ports,
  developer tools. One stops at the first problem and the other collects
  them. They have already drifted: only provisioning checks Tcl packages,
  the compiler, and the agent version. Both write the manifest path
  `/opt/dockhand/image.json` as a literal.
- *`tart/provision` re-implements Tart VM operations that `tart/host.Machine`
  owns.* It has its own clone (without host's refusal to overwrite), stop,
  image listing, and agent readiness wait. It also builds a throwaway
  `Machine` on each call rather than holding one.
- *Two revision editors.* `portfile.BumpRevision` finds the revision by
  scanning syntax, and `portedit/revision_reset.go` finds it by
  declaration frames. The second is pure (contents and declarations in, a
  text edit out) and belongs in `portfile` beside the first.
- *Index staging.* `portindex.Stage` followed by `portindex.Open` is
  written in `app/selection.go`, `dependents/discover.go`, and twice in
  `survey`.
- *Fork ownership.* `publish.Destination` already fetches the head
  repository and checks its owner, and `app/github_verification.go`
  fetches it and checks again.

**An agreement that holds only by coincidence.** GitHub verification
locks a fork branch under `verify.ProviderGitHub`
(`verify/github/provider.go:147, 205`), and publication locks the same
branch under `action.Spec.Forge` (`workflow/publication_run.go:79`). They
exclude each other only because both strings are `"github"`.

**One HTTP transfer outside `fetch`.** Every download goes through
`fetch.Open`'s bounds and status handling except GitHub Actions job logs.
`verify/github/actions.go:97` uses `http.DefaultClient`, ignoring the
injected client, with no size limit. `cacheJobLog` then copies the body
to disk without a bound. Subprocesses are consistent. The exceptions to
`subprocess` are interactive (the editor, `auth`, the browser) or
long-lived (the Tcl shell, the foreground VM), plus `credential/keychain`
and `gh auth token`.

**Request options cross four layers by hand.** `KeepFailed`,
`IncludeDependents`, and `AllSubports` are each declared in seven
structs, from `app.Preparation` through the three workflow requests to
`record.JobSpec` and `sqlite.jobOptions`. `jobOptions` copies 17 of
`JobSpec`'s 23 fields in each direction. There are two `AdoptRequest`
types. Preparation and verification go from cli through `app`, while
correction and publication build their workflow requests in cli
directly.

## 2. Where logic concentrates

**`workflow.Engine`: 19 fields, 65 methods, and 27 more on `cycle`.**
Counting which fields each file reads gives six clusters, each needing a
narrow part of the engine:

| cluster | fields | lines |
| --- | --- | ---: |
| records only: status, control, selection, submit, scope | State, Repository, Now | 1,073 |
| pull request lifecycle: lifecycle, branches, adopt, continuation, cleanup | State, Repo, `Publisher.Forge`, PullRequestInterval | 1,168 |
| binding: the four bindings, reassociation, release scope, resolution | State, Repo, Ports, Workspaces | 1,534 |
| preparation run and integration | Preparer, Releases, Repo | 477 |
| verification driver | Providers, Dependents | 1,353 |
| publication | Publisher | 759 |

Three facts sharpen this:

- `State` and `Repository` travel together in all 62 state calls, and the
  guard `e == nil || e.State == nil || e.Repository == ""` is written 15
  times.
- Seven of the nineteen fields (Owner, Timeouts, LeaseGrace, RetryDelay,
  PullRequestInterval, WaitInterval, ObserveInterval) are set only by
  tests; `app` sets none of them.
- `workflow` reaches into `publish.Service` for `Publisher.Forge` 13 times.
  Most of those uses observe pull requests and have nothing to do with
  publishing.

**Commit-message policy lives in the Portfile editor.**
`portedit/message.go` holds `Subject`, `Rewrite`, `IsAttribution`, and
`GeneratedBy`. `workflow` imports `portedit` for the first two, and
`publish` imports it only for `IsAttribution`, a ten-line prefix check.
That one predicate gives `publish` a closure of 28 internal packages,
more than `verify/tart`'s 22. It is also the only reason `portedit`
imports the build's `version` package. Both teams found this
independently.

**`selection.Reader` quietly does four jobs.** It embeds
`*eval.Evaluator` and becomes `app`'s target resolver, its native-platform
source (six call sites), and, through promoted methods, `upstream`'s
version comparator.

**`eval` is mostly static analysis.** 936 of its 1,635 lines are the
pre-fetch guard grammar (`effect.go`, `fetch.go`) with one call site,
`assessFetch`, and the root package's `fetch_effect.go` serves only it.
The grammar needs no interpreter, yet its tests run inside the
interpreter's package.

**`git/changeset` contains MacPorts rules.** It knows `_resources`,
the category/port layout (`ScopeOf`, `Scope.Portfile`), and how to find
PortGroup users. `components.md` says it interprets no MacPorts paths.
The code or the document is wrong, and the code is right.

**`cli` repeats its submission tail four times.** Build, submit, report,
choose a milestone, attach: see `actions.go`, `preparation.go`,
`correction.go`, and `publication.go`. The copies already differ:
correction returns submit's error without the "accepting request"
wrapping the others add. This is the command pipeline the roadmap
carries.

Not concentration: `git`'s fan-in of 21 belongs to a leaf that imports
three utilities, and five of its importers use only the `Valid*`
predicates. `verify/tart`'s fan-out comes from staging the source, with
one leak: `app/dependents.go` calls `verify/tart.SourceIndex` to derive
index settings.

## 3. Contracts worth declaring

Each of these removes a real dependency or a real duplicate. None is a
split by file count.

1. **`state.Scoped`**, with `View` and `Update` bound to one repository
   by `state.Bind(store, id)`. The engine, `retention.Collector`, and the
   records cluster take it. It removes 62 repository arguments and 15
   guards, and it is the first step toward the records cluster leaving
   the engine the way `proc` already did (`proc` is 120 lines behind a
   two-method `Engine` interface, and is the model).
2. **Forge contracts in `forge`**: `PullRequests` (Find, Observe, Create,
   Update, with `PullRequestInspector` optional) and `Accounts` (Name,
   AuthenticatedUser, NameFromRemote, RepositoryInfo).
   `forge/github.Client` already implements both. `publish`, the pull
   request lifecycle cluster, and `app` would take them directly instead
   of reaching through `Publisher`. The value types already live in
   `forge`, so only the interfaces move.
3. **A verification execution ledger**, as a leaf both providers use
   (for example `verify/ledger`, outside `verify`, whose import test
   allows only `record` and `git`): lock, read, put, close-unknown, read a
   log chunk. It removes the duplicated ledger and keeps the
   closed-request rule in one place.
4. **An index source**:
   `portindex.Source{Index(ctx, macports.Tree) (*portindex.Index, error)}`,
   implemented by a stager that remembers what it staged. That absorbs
   `app/selection.go`'s staged-index map, a carried roadmap item.
   `selection`, `dependents`, and `survey` take it, and tests can pass a
   fixture index.
5. **`selection.Reader` over `macports.NativeEvaluator`** rather than the
   concrete evaluator, with `app` passing the evaluator to `upstream`
   directly. `app` becomes `eval`'s only importer, and the selection
   tests stop needing a real MacPorts.
6. **One image inspection in `tart`**:
   `InspectImage(ctx, run, prefix) → ImageFacts{Manifest, Capabilities, Problems}`
   and a manifest path constant. Provisioning turns problems into an
   error, and verification records them.
7. **`record.BuildChoices`**, embedding KeepFailed, IncludeDependents,
   AllSubports, Tests, and FromSource in the requests, `JobSpec`, and
   `jobOptions`. These are the cohesive values the 09-22 review asked for.
8. **Two leaves, not contracts**: a commit-message package (`Subject`,
   `Rewrite`, `IsAttribution`, `GeneratedBy`, depending on `record` and
   `version`), and `macports/fetchguard` for the guard grammar. Moving
   `tart.BuildOptions` and Tart's two unavailability errors into `verify`
   makes `workflow/choice` the policy leaf it was meant to be.

Not recommended: splitting `state.Reader`, since only workflow consumes
it; one observation interface over both providers, whose verdict logic
differs by design; an interface over `portedit`, whose consumers already
take the narrow `ProbeSource`.

## 4. What can go

Each item was checked by reference search, and the unreachable functions
by `deadcode` as well.

**The `review` stub.** This covers `cli/commands.go`'s review command
and the "planned" help group, `record.ReviewAccept`/`ReviewDismiss`, and
`ControlRequest.ChangeID`/`ExpectedRevision`, which are only ever checked
to be empty in three places. It is also not inert: `dockhand review
accept X` builds the services, creating and registering the database,
before saying it is not wired.

**A roughly 200-line fallback that cannot run.** In `applyArchivePlan`,
the branch after the observed and Git plans (`portedit/version.go:129-141`)
is unreachable. Every plan that reaches it carries observed archives or
goes through Git, and "no update" returns first. Deleting it removes
`archives.Store.Refresh` and `CheckChecksumSources`,
`portfile.ReplaceChecksums`, `ReplaceChecksumsKeeping`, `checksumGroups`
and their types, and `fidelity.Checksums`. `deadcode` cannot see this,
because a runtime condition, not a missing caller, is what keeps the
branch from running. Add a test that proves the branch is unreachable,
then delete.

**Fields and methods nothing uses:**

- `state.Query`'s `Newest`, `WithBuild`, and `Action`: never set.
- `workflow.PreparationRequest.Selection`: never set or read.
- `app.Services.Preparation`: never read.
- `record.VerificationTarget.Prerequisites` and `.Inputs`, and
  `ArtifactRequirement`: no references. `BuildSpec.Inputs` is always
  empty, and both providers refuse it when it is not.
- `state.Reader.Revisions` and `SubmissionsForAttempt`: test fixtures
  only.
- `macports.NewContext`, `macports.NativeReader`, `eval.pureCondition`:
  no callers.
- `Engine.publicationDestination`, `workflow/cycle.due`,
  `assess.Journal.Held`, `cli.NewRoot`: unreachable from `main`.

**Test-only code in production files.** Move these into `_test.go`, or
point their tests at the production entry point:

- The guard grammar's `rejectionOnly`, `conditionalRejection`,
  `goToolchainCheck`, and `parseHook` wrap `classifyHook`, so their tests
  exercise wrappers rather than what production runs.
- `verify.PlanSingle` and its helpers; its comment says evidence reuse
  uses it, and nothing does.
- `upstream.PatternFromCurrent`; `macports.KnownOption`, `InfoOptions`,
  `ComputedOptions`; `syntax.SpanOf`, `SegmentSpan`; the seven functional
  options of `tcl/rpc` and `tcl/shell` that production never passes.
- Plain-directory trees: every production tree comes from a workspace, so
  `macports.NewTree`, its `whole` projection, and `Tree.Projected()` are
  for tests, and `app/selection.go`'s `ValidatePortsTree` branch never
  runs.
- `Engine.ControlBranch`, `BranchScope`, `PreparationInput`, and
  `CheckContinuation` are exported for tests only. `workflow/preparation`
  re-exports `CommitIntent`, `GeneratedBy`, `ErrUnsupported`, and
  `ErrFidelity` for tests, while `app` imports `portedit` directly for
  `ErrUnsupported`.

**`Engine.Provider`.** Its one production read is a fallback that `app`
never reaches, since `app` always sets `Providers`. The cost of removing
it is 26 test literals.

**Aliases and small leftovers:**

- `upstream.ErrVersionInput` aliases `version.ErrInput`, and
  `dependency.ErrManifestMissing` aliases `macports.ErrManifestMissing`.
- Three `digest` helpers exist in `tart`, `verify/tart`, and
  `verify/github`.
- `/var/tmp/dockhand2` is written twelve times in `verify/tart/native.go`
  beside a `guestDirectory` constant.
- `scratch` calls `syscall.Flock` rather than `filelock`.
- `NewTreeOver`'s documentation paragraph appears twice.
- Names exported without outside users: `git.WithPushLock`,
  `upstream.Documents`, `upstream.VersionSelector`, `host.GuestCommand`,
  `StubMembers`, `NaturalCompare`, `TracURL`.

**Packages that could fold:**

- `tcl/shell` (224 lines) serves only `rpc` and `eval`, and can become
  `rpc.Start`.
- `outdated` (133 lines) repeats `assess`'s survey setup and is still
  sequential where `assess` uses a pool; it can become a mode of
  `assess`.
- `selection` (75 lines) earns its place only if contract 5 is made.

`forge/gitlab`, `credential/keychain`, `testsupport`, `proc`, and `tui`
were checked and stay: each has a production caller or a real boundary.

**Documentation that has drifted.** `components.md` omits `scratch`,
says `changeset` interprets no MacPorts paths, and says `portedit`
imports no Git package; it uses `git.FileEdit`.

## Suggested order

1. **Defects first.** Read Tcl booleans one way (the `use_xcode on` case
   records the wrong scope). Lock fork branches under one key. Route the
   job-log download through `fetch` with a bound. Each is small.
2. **Deletions.** The `review` stub, the unreachable archive fallback
   (with its guard test), the unused fields and methods, and the
   test-only code moved out of production files. This shrinks the
   surface before anything moves.
3. **Leaves that cut closures.** The commit-message package and
   `macports/fetchguard`, then `BuildOptions` into `verify`. Mechanical,
   and `publish` drops from 28 dependencies to a handful.
4. **Single rules.** `PortInfo.Bool`, `JobPhase.Next` and `ValidCancel`,
   one list of preparing actions, one master fetch, one checksum
   vocabulary, `syntax.Command.Control` in the guard grammar.
5. **Contracts, one at a time:** `state.Scoped`, then the forge
   contracts, then the ledger, then the index source. Each lets a
   workflow cluster or a provider take only what it uses.
6. **The request values** (`BuildChoices`) and the cli submission
   pipeline, once the command surface settles, as the roadmap already
   says.
