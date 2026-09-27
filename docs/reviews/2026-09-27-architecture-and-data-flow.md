# Architecture and data-flow review

Reviewed 2026-09-27 at `e8e62eb415096f13ca24fbf60e4d110554410ede`.

The review started at `6460c3a7`, the Golden Gate/Tart image change. HEAD
advanced to `e8e62eb4` during inspection; that commit changes an explanatory
comment and an activity note. The review and regression probes are pinned to
an exported copy of `e8e62eb4`. Subsequent working-tree edits are outside this
review. This reviews the architecture at that revision, not just its commit diff.

## Assessment

The v3 branch/revision/plan/run model is a sound foundation. The rewrite has
improved the separation between editing, checking, shaping history, and
publishing. The weak point is now the information passed between those
activities: several adapters reduce a richer domain fact to a name, subject,
boolean, or aggregate verdict, and later code reconstructs meaning from that
reduction. Some of those reductions already produce incorrect behavior.

The priority is to give **check policy, environment-specific planning, and
recoverable history changes** clear owners. Those are useful extraction
boundaries for the growing engine. A broad package reshuffle before fixing their
contracts would move the same problems into more directories.

Seven focused probes reproduce the behavioral findings below. The existing
core, command, provider, and Tart suites pass. The probes are in the companion
[patch](2026-09-27-architecture-regression-probes.patch), not installed in the
production test suite.

### Size and dependency map

These are physical lines in non-test `.go` files, including comments and all
platform-specific files, measured at the reviewed revision. They are indicators
of responsibility, not quality scores.

| Package | Production files | Lines | Assessment |
| --- | ---: | ---: | --- |
| `internal/engine` | 30 | 9,329 | Main concentration of unrelated responsibilities |
| `internal/command` | 28 | 6,716 | Large, but much is legitimate CLI parsing and presentation |
| `internal/macports/portedit` | 21 | 3,025 | Substantial domain implementation with existing supporting packages |
| `internal/git` | 15 | 2,562 | Generally coherent adapter boundary |
| `internal/macports/portindex` | 10 | 2,013 | Coherent index/workspace concerns |
| `internal/tart/provision` | 11 | 1,473 | Reasonably focused; image vocabulary is duplicated elsewhere |
| `internal/store/sqlite` | 4 | 1,245 | Not currently a monolith requiring another abstraction layer |
| `internal/model` | 9 | 1,212 | Useful dependency-free vocabulary and validation |
| `internal/provider/tart` | 5 | 1,029 | Good provider boundary; 647 lines concentrate in `provider.go` |

`go list -deps ./cmd/dockhand` reaches 60 internal packages. Twenty-three of
those still directly import `record`. Fifteen other internal packages, excluding
test support, contain 18,225 production Go lines outside the CLI dependency
graph. They are **not all dead**: `tools/survey` uses `assess`, and
`tools/stateperf` uses the old state/workflow stack. This distinction matters
when planning v2 removal.

The four findings from the
[September 25 implementation review](2026-09-25-v3-implementation-review.md)
have corresponding fixes in this revision. This report does not repeat the old
unchecked-coverage, shared-code classification, staged-index preservation, or
first-platform-only dependency defects as if they were still unfixed.

## Findings

### 1. [P1, reproduced] Test policy has no single owner from provider to publication

Sources: [Actions execution and verdict](../../internal/provider/actions/provider.go#L94),
[result recording](../../internal/engine/runner.go#L457),
[evidence merging](../../internal/engine/evidence.go#L208),
[result wording](../../internal/engine/words.go#L12), and
[PR test summaries](../../internal/engine/body.go#L288).

`Plan.Tests` is accepted and persisted, but enforcement depends on the adapter.
Tart interprets `required` in its guest program. Actions computes a verdict from
build markers without consulting `job.Plan.Tests`. The driver accepts the
provider's verdict without applying the requested test policy.

A command-level probe using the existing local Git and fake Actions fixtures
produces this output from `check --on github --tests required`:

```text
Provider    github · tests required
  jq  ✓ build passed; tests failed (advisory)
Passed for snapshot 1.
```

The command returns success. This contradicts both the requested policy and
[`github-provider.md`](../github-provider.md). A substantive target can therefore
be recorded as passing despite a test failure the person explicitly made
decisive.

Two other paths show the same architectural problem:

- Cross-run reuse matches target ID and environment after filtering by source
  tree, without checking test policy. After a full advisory check and a focused
  required-tests check, the aggregate can say `Plan.Tests=required` while an
  omitted target has `Passed=true, Tests=failed` and submission has no blockers.
  Whether an earlier target keeps its original policy or must satisfy the new
  policy needs an explicit rule; the current aggregate loses that distinction.
- `TargetWords`, `testsDeclared`, and `testsPassed` do not consistently account
  for `TestsTimedOut`. A passing build with timed-out advisory tests renders as
  an ordinary tick. With another target whose tests passed, the generated PR
  also ticks the test checklist item. The raw timeout survives storage, but its
  meaning disappears from the report.

**Recommendation:** introduce one check-policy implementation, consumed by the
driver, evidence aggregation, and presentation. It should distinguish build
outcome, test outcome, and the verdict under a specified policy. Providers
report facts; a common function judges those facts. Providers that cannot honor
a requested behavior, such as skipping workflow-owned tests, should expose that
capability explicitly.

Give evidence reuse an explicit compatibility predicate. Whole-tree reuse can
remain the conservative rule while checking target selection, environment, and
test-policy compatibility. Do not require completion of fine-grained input
reuse to fix this. Preserve the contributing plan's policy when reporting mixed
evidence, or reject/rejudge incompatible evidence under the requested policy.

Probes: `GitHubHonorsRequiredTests`, `ReuseHonorsTestPolicy`, and
`TimedOutTestsAreNotPassing`, all prefixed `TestArchitectureReview`.

### 2. [P2, reproduced] A plan is several partially synchronized views of an environment

Sources: [PlanCheck](../../internal/engine/plan.go#L38),
[Plan's representation](../../internal/model/plan.go#L124),
[evaluatedPorts.Ports](../../internal/engine/ports.go#L53), and
[PlanBaseline](../../internal/engine/baseline.go#L29).

The main target list is a union across environments. Dependencies are a
parallel slice indexed by environment; `DependsOn` is also their union;
`NeedsXcode` is on the target; `Unmet` is on the plan; exclusions name a platform
but not the full environment. `Validate` checks some relationships, but does
not make these a complete account of each target in each environment.

Three concrete consequences are reproducible:

1. **A subport absent from one evaluation is treated as buildable there.** The
   planner records an exclusion only for a returned `PortInfo`. A subport
   defined only on x86_64 enters the union, but receives no arm64 exclusion.
   The arm64 job consequently includes a target its evaluation never defined.
   The probe uses a platform-sensitive reader, not a live Portfile execution.
2. **Baseline construction bypasses the environment rules.** It copies targets
   and filtered dependency maps into a new plan, without its exclusions or
   `Unmet` entries, and without evaluating the base. A target can retain
   `NeedsXcode` for a CLT environment while `UnmetIn` returns false; the driver
   consequently sends it to that environment. Simply copying the old exclusions
   is insufficient, since the base may evaluate differently.
3. **A baseline of an old check uses the mutable branch's current base.** After
   checking, rebasing, and asking for a baseline without another check,
   `Baseline.Of` still identifies the old check, while `Revision.Source.Commit`
   names the newer master. The comparison has lost the original revision's base.

There is also an acknowledged limitation in the current representation:
opposite dependency edges on different platforms create a cycle in the global
union. The code safely refuses it and asks for separate checks. That is an
honest fallback, but a per-environment build order would remove the limitation.

**Recommendation:** model an `EnvironmentPlan` containing its evaluated target
membership, eligibility reasons, requirements, dependency graph, and build
order. Keep branch-wide changed scope and the person's selection separately.
An absent target must have an explicit status rather than inheriting presence
from another platform. Key environment-specific facts by the full environment,
not by independently maintained positional slices.

Extract evaluation/planning so normal checks and baselines call the same path.
A baseline supplies the original checked revision's base as its source and
requests the relevant targets explicitly. It should not synthesize a plan by
copying only selected fields from another source's plan.

Probes: `AbsentSubportIsNotBuilt`, `BaselineKeepsEnvironmentRequirements`, and
`BaselineUsesCheckedBase`.

### 3. [P2, reproduced plus recovery risk] History changes need a complete state transition

Sources: [Checkpoint](../../internal/model/history.go#L113),
[Rebase](../../internal/engine/verbs.go#L121),
[ApplyTidy and Restore](../../internal/engine/tidy.go#L576),
[the transaction contract](../../internal/store/store.go#L5), and
[SQLite commit uncertainty](../../internal/store/sqlite/sqlite.go#L279).

A checkpoint holds old/new heads and the pre-tidy index. Rebase additionally
changes `Branch.Base`, but neither side of that base transition is recorded.
`Restore` restores the head and index and marks the checkpoint restored; it
does not restore the base.

The probe rebases onto a newer master and restores the checkpoint. HEAD returns
correctly, but the stored base remains the newer master. A subsequent tidy fails
with “is not above its base.” The existing rebase test checks restored HEAD and
misses the rest of the branch state. This is distinct from the staged-index
problem fixed on September 25.

The surrounding code also needs a clearer recovery boundary. The store contract
says callbacks never call Git, but `ApplyTidy` updates refs inside the SQLite
transaction, and rebase creates its checkpoint ref there too. A SQLite
transaction cannot make those Git changes atomic. Ordinary returned errors have
some compensating logic, but a process death between the ref update and database
commit bypasses it. Further, tidy attempts compensation for every transaction
error even though the store can return `ErrUncertain`, meaning the database
commit's outcome is unknown. These crash/uncertain-commit observations are code
inspection findings; this review did not inject process death or a failing
SQLite COMMIT.

**Recommendation:** give history operations a complete before/after state
containing at least head, base, and the index information relevant to restoration.
Add a small durable operation record: prepare intent, perform guarded Git
changes, observe their outcome, then finalize the stored state. On an uncertain
commit, reread before compensating. Recovery should be idempotent and preserve
the existing compare-and-swap checks on refs.

This is a good boundary for a focused history service covering tidy, rebase,
restore, and checkpoint recovery. It does not require resurrecting v2's general
workflow engine or making every authoring action a queued job.

Probe: `RestoreRestoresBase`.

### 4. [Structural priority] `engine` is becoming the replacement monolith

Sources: [Engine and its construction](../../internal/engine/engine.go#L63),
[provider contracts](../../internal/engine/provider.go#L16),
[CLI composition](../../internal/command/settings.go#L79),
[lazy domain construction](../../internal/engine/preparer.go#L34), and
[the command boundary test](../../internal/command/boundary_test.go#L16).

The 9,329-line engine imports 29 other local packages directly. It owns branch
discovery, authoring integration, graph planning, run driving, evidence policy,
history rewriting, PR composition/publication, review, cleanup, and scheduling.
Its largest files are `runner.go` (826 lines), `tidy.go` (767), `submit.go` (636),
`serve.go` (594), and `clean.go` (588). The concern is the number of independent
rules sharing one package and one mutable `Engine`, rather than any one length.

The command boundary test is valuable, but its practical direction is “move the
decision into engine.” There is no complementary rule limiting what engine
itself owns. Composition is split too: command constructs providers, engine
opens SQLite and constructs MacPorts and forge adapters, and a provider imports
engine to name `Job`, `Build`, and `Fork`. Those upward dependencies would make
a naive engine subdivision awkward.

**Recommendation:** keep `Engine` as the application facade, then extract these
bounded responsibilities in order:

| Boundary | Responsibility | Dependency rule |
| --- | --- | --- |
| Check policy/planning | Environment plans, result interpretation, evidence compatibility and publication coverage | Depends on model and narrow evaluation inputs, not CLI or concrete providers |
| Execution contract/driver | `Job`, reporting/checkpoint interface, retries, cancellation and run settlement | Providers consume this contract without importing the application facade |
| History service | Complete checkpoints, tidy/rebase/restore and reconciliation | Owns guarded Git transitions and its store operations |

Package names such as `check`, `execution`, and `history` are suggestions; their
contracts matter more than spelling. Move the provider contract before moving
the driver. Keep production wiring together behind the existing opening path,
with constructors that accept dependencies for tests. Split broad store/forge
interfaces by the needs of an extracted consumer, rather than inventing another
general repository abstraction.

Do not split every CLI verb into its own package. `command` is large but has a
credible responsibility and a boundary test. Likewise, `portedit` already
delegates observation, archive handling, syntax edits, fidelity, and dependency
regeneration; its size alone does not justify another breakup.

### 5. [P2, lossy contract] Provider execution is not always one observed environment

Sources: [Actions runner aggregation](../../internal/provider/actions/provider.go#L188),
[GuestExecution and TargetResult](../../internal/model/execution.go#L48),
[Tart's guest facts](../../internal/provider/tart/guest.tcl#L194),
[Tart result conversion](../../internal/provider/tart/provider.go#L571), and
[Evidence.Observed](../../internal/engine/evidence.go#L71).

The execution model fits a Tart clone well. A GitHub workflow execution actually
contains several runner jobs, but `verdict` folds them into one target result,
one phase, one tests value, and one log path. Runner job IDs/names are not
retained in the stored result. The raw log files remain on disk, but normal
result queries cannot show which platform passed, failed, or never listed the
port. Indeed, a port absent from any runner yields no aggregate verdict, even
when absence may reflect platform-specific eligibility.

The local provider has smaller reductions at the same boundary. The guest
reports MacPorts version, developer directory, and failure detail. `reported`
keeps only the fields that fit `model.Observed`; per-target detail becomes a
progress message, not a structured result field. Across reused attempts,
`Evidence.Observed` selects the latest nonempty observation for the whole
environment, even if some target results came from an earlier execution with
different observed tools. The executions themselves retain their observations,
so the last case is a reporting reduction, not deletion from storage.

**Recommendation:** distinguish the provider operation from its observed
builders. Retain per-builder observations/results, including the provider's
runner identity and log reference, then derive the summary. For Tart, the
operation has one builder. For Actions, it has the workflow's matrix jobs.
Preserve structured result detail and enough environment facts to explain the
run, including the MacPorts version already reported by the guest. Link any
reported environment summary to the executions that actually supplied it.

`PortReader` similarly reduces a full snapshot/observation to `[]PortInfo`
([ports.go](../../internal/engine/ports.go#L100)). Fine-grained input reuse is
explicitly deferred, so its absence is not a current defect. When it lands,
this interface must return an evaluation report or observation reference; the
ledger cannot be reconstructed from the port names and dependency lists later.

### 6. [P2, lossy contract] The durable authoring record is too small for the editor's report

Sources: [preparation.Result](../../internal/preparation/preparation.go#L28),
[portedit.Result and CommitIntent](../../internal/macports/portedit/prepare.go#L28),
[describe/editRecord](../../internal/engine/update.go#L267),
[model.Edit](../../internal/model/history.go#L33), and
[updateView](../../internal/command/json_results.go#L67).

The editor returns release membership, evaluation coverage, fidelity reports,
the exact selected release, patch results, and commit intent. The preparation
adapter carries most of those fields forward. The v3 adapter then narrows them:

| Fact produced | What survives |
| --- | --- |
| `ReleaseScope` with affected/protected members | Not carried by `engine.Update` or `model.Edit` |
| Context coverage and fidelity | Used partly for displayed before/after versions; not retained as an edit report |
| Selected release, including tag, upstream commit and discovery evidence | Present in transient `engine.Update.Release`; absent from the stored edit and update JSON |
| Commit intents with subject, body, references and paths | Only the first subject is selected; stored edit has no full intent |
| Patch-check findings | Flattened into immediate text/JSON; absent from the stored edit |
| Upstream archive comparison | Stored on the edit, but omitted by `updateView`, despite text rendering it |

This does not mean file changes are dropped: all `result.Files` are applied and
recorded. Nor should old v2 release membership dictate v3's build scope, which
properly derives from the diff. The lost information is the explanation of what
the helper did and how it established it. A later status/tidy/submit process
cannot recover the original release observation or authoring findings from the
small edit row. Some richer commit-intent fields are not currently populated by
the v3 CLI; those are contract limitations, not a claim that an existing flag
loses a supplied ticket.

**Recommendation:** define a compact `EditReport` shared by the preparation
adapter, engine response, and durable edit. Retain source identities, selected
release provenance, affected members, structured findings, and full applicable
commit intent. Keep large snapshots, temporary paths, and downloaded archives
ephemeral. Let JSON/text views project the report deliberately, and document
fields omitted from a public output. Add an update → reload → tidy/status test,
not just separate tests of the editor and CLI renderer.

### 7. [Structural priority] Complete the vocabulary migration before removing the old engine

Sources: [record aliases](../../internal/record/source.go#L9),
[model vocabulary](../../internal/model/vocabulary.go),
[forge input](../../internal/forge/pullrequest.go#L42),
[staging](../../internal/verify/staging/archive.go), and
[roadmap](../roadmap.md).

There are two different situations in the v2 remainder:

- Old orchestration/storage/provider implementations are outside the CLI graph:
  `workflow`, `state`, `publish`, most of `verify`, and their supporting packages.
  Their tests and `tools/stateperf` still compile them.
- `record` remains a live domain dependency. `Source`, `Target`, `Platform`, and
  `ObjectID` already alias model types, so these are **not divergent copies**.
  But action/edit intent, release/scope, references, and forge publication types
  are still genuinely owned by the old package.

This leaves a misleading import boundary: a fresh v3 domain adapter imports a
package that also defines retired jobs, attempts, leases, and workflow state.
Likewise, the new Tart provider imports `verify/staging` from beneath the old
verification namespace. A maintainer has to know the migration history to choose
the active implementation.

Some old fields also advertise guarantees their v3 caller does not use.
`forge.PullRequestInput` still carries `ActionID` and `ExpectedRemoteHead`; v3
does not populate them in `ApplySubmit`. Git pushes have a separate real guard.
The mere presence of these fields should not suggest that the old publication
recovery protocol survived the rewrite.

**Recommendation:** make the exit criteria explicit. Move alias-only callers to
`model`; move forge types into `forge` and edit contracts toward their domain
owner, using a small neutral release contract if moving them into an existing
package would create a cycle. Avoid dumping retired workflow records into
`model`. Move the still-used staging utility to a neutral source-staging home.

Inventory dependencies from the CLI, tools, and tests before deletion. Preserve
`assess` for the live survey or deliberately replace that caller. Port the
specific v2 recovery acceptance cases that still define v3 promises, then retire
the obsolete implementations and either replace or retire `tools/stateperf`.
Add a dependency check preventing new CLI-reachable imports of the retired
packages. The existing command-layer import test shows a suitable precedent.

### 8. [P3, duplication] Image naming and capabilities should have one descriptor

Sources: [shared Tart release helpers](../../internal/tart/release.go#L28),
[v3 provider naming](../../internal/provider/tart/provider.go#L161),
[setup naming](../../internal/provider/tart/setup.go#L55), and
[provisioner naming](../../internal/tart/provision/provision.go#L255).

Base/Xcode image names already have helpers in `internal/tart`, but the v3
provider reconstructs them. Golden-image names are independently constructed in
provider setup and provisioning. Availability is then converted into developer
tools in one place and setup status in another. The implementations presently
agree; this is duplication to remove, not a reproduced naming failure.

Introduce a small shared image descriptor in `internal/tart`: release, profile,
prepared name, golden name, and source selection. Setup, status, environment
resolution, and cleanup can consume that descriptor while retaining their own
operational behavior. A new generic VM framework would be disproportionate.

The reviewed Golden Gate change is otherwise placed sensibly: Tart-specific
listing retry/classification belongs in `tart.Client`, ASIF setup constraints
belong in provisioning, and toolchain facts belong in `macos`. Those changes
should not be pulled upward into engine during the refactor.

## Suggested implementation order

1. Fix required-test enforcement, timeout reporting, restored base, and baseline
   source binding with focused regressions. These need not wait for package moves.
2. Introduce one environment-plan construction path and one evidence-policy
   definition. Use the absent-subport and baseline-requirement probes to hold
   their boundaries. Preserve the existing narrowed-coverage regressions too.
3. Complete history checkpoints and add durable reconciliation around Git/store
   transitions. Test restart and uncertain-commit behavior explicitly.
4. Extract the provider execution contract and the check/history services behind
   the current engine API. Require these packages to avoid importing command or
   the facade. Keep the CLI behavior stable during these moves.
5. Retain compact edit reports and per-builder evidence, finish vocabulary
   migration, and remove obsolete v2 implementations once their callers and
   acceptance tests have been accounted for. Consolidate image descriptors as a
   small independent change.

The useful tests at these boundaries ask whether meaning survives a round trip:
check → result → submit; update → stored edit → tidy; rebase → restore → check;
and interrupted execution → resumed execution → evidence. Package-local happy
paths can all pass while these transitions lose context, as the probes show.

## Validation and limits

The following existing suites passed in the exported revision, using Go 1.27.1:

```sh
go test ./internal/model ./internal/store/... ./internal/coord \
  ./internal/engine ./internal/command ./internal/provider/... ./internal/tart/...
```

Engine and command completed in approximately 78 and 88 seconds. An initial
sandboxed attempt could not open the mock HTTP servers' loopback listeners;
the successful run allowed that test requirement. Caches were placed in `/tmp`.
No real provider build, image setup, upstream publication, or user ports-tree
mutation was performed. This was not a full race-detector, MacPorts evaluator,
or every-package test run.

To reproduce the seven additional probes, apply the companion patch to an
isolated export of `e8e62eb4` and run:

```sh
go test ./internal/engine ./internal/command \
  -run '^TestArchitectureReview' -count=1 -v
```

All seven probes assert the intended behavior and fail on the reviewed revision.
Six exercise the engine with local fixtures or generated PR text; one exercises
the command → real Actions adapter → engine path with a fake API and local fork.
They establish the stated implementation behavior, not live VM or GitHub
certification. Structural recommendations and untested recovery risks are
identified separately above.

Only this review, its companion patch, and an activity note were added by this
review. Production code was not changed, and no commit or push was made.
