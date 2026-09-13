# Initial component structure

This implementation map follows the [architecture](architecture.md), [principles](principles.md), [CLI design](cli-design.md), and [state-store design](state.md). The state design replaces Git-ledger persistence with repository-scoped contracts and a SQLite backend. The initial SQLite migration is implemented; groundwork status below records the remaining execution gaps.

Use one Go module and one executable. Start with a shared workflow engine, capability packages, and concrete integrations. Add files as behavior is implemented; this tree is not a request to create empty packages or implement phase two immediately.

## Package map

```text
dockhand2/
  cmd/dockhand/
    main.go
  internal/
    app/                 # Configuration, setup, and dependency construction
    cli/                 # Command parsing, human/JSON output, attachment
    proc/                # Current-process driver lifetime and residency
    record/              # Shared durable records, identities, and value types
    state/               # Repository-scoped persistence and transaction contracts
      sqlite/            # SQLite storage, connections, and schema migrations
    workflow/            # Request acceptance and all workflow advancement
    prepare/             # Source transformations and edit-fidelity checks
    upstream/            # Release discovery and version assessment
    verify/              # Build specifications, coverage plans, verdicts
      tart/              # Concrete VM verification provider
    publish/             # Publication policy, desired state, reconciliation
    macports/            # Bound source contexts, evaluation, dependencies
    tcl/                 # Tcl process/RPC support and source syntax tools
    text/                # Byte spans and source-preserving edits
    git/                 # Git objects, refs, snapshots, guarded remote pushes
    forge/
      github/            # Release and PR API adapters
  docs/
  go.mod
```

The initial internal files can be straightforward: `workflow/submit.go`, `cycle.go`, `prepare.go`, `verification.go`, `publish.go`, `cleanup.go`, and `status.go`. They share the same workflow engine and transaction rules. A separate Go package is justified by a useful dependency boundary, not by every lifecycle noun or CLI verb.

## Responsibilities

### Entry points and process lifetime

`cmd/dockhand` contains executable startup and exit handling. `app` resolves configuration, discovers the selected repository, constructs dependencies, and owns their lifetime. Cobra passes global `--db PATH` through `app.Config.DBPath`, defaulting to `$HOME/.dockhand/state.db`. Open state lazily: writable operations initialize it and register a repository; status uses read-only lookup and does not create missing state. Help, completion generation, and previews do not open it. `app` injects a `state.Store`; it must not acquire a second workflow sequence as commands grow. No config-directory concept or lock-directory flag remains in this design.

`cli` uses Cobra for the command tree, flag parsing, argument validation, generated help, and shell completion. It parses commands into typed requests and renders typed results. It owns human output, JSON output, exit-code mapping, and the choice to observe admission or completion. It submits requests through the shared workflow API rather than writing record shapes itself; it never settles an attempt or performs driver bookkeeping. Domain packages do not print terminal messages or decide exit codes.

`proc` manages residency and persistent execution within the current `dockhand` process. Change commands run targeted workflow cycles in their invocation; `dockhand start` explicitly runs persistent mode for the selected repository. No separate executable, executable-path discovery, or automatic child driver launch is needed. The database's repository ID comes from canonical Git common-directory registration. Linked worktrees share an entry; separate clones remain distinct. Database selection is independent of the checkout.

The state store is the request handoff and progress channel. The CLI calls `workflow.Submit` in its own process to validate and transactionally persist a queued job, then runs targeted workflow cycles in that invocation. A resident driver or targeted cycle reads eligible work from the state store and claims it transactionally. There is no socket or separate request transport. Action invocations and explicit persistent mode execute the same `workflow.Engine`.

Successful durable submission establishes acceptance and returns the job ID; it does not mean a driver has claimed the job or a provider has admitted a build. Request IDs allow an interrupted caller to find its recorded submission without duplicating it. If the invocation ends before finishing its work, the request remains recorded and recoverable without claiming admission or completion. Cancellation and review requests use the same workflow-owned intake path; the driver applies their consequences.

The CLI observes progress by reading state through a shared read-only status projection. While waiting, the invocation can also run targeted cycles through the workflow engine; those cycles retain all bookkeeping responsibility. `wait` repeatedly reads the selected jobs until their requested milestone is recorded; `status` works even when no driver is alive. Preserve observation timestamps and do not poll a provider or run bookkeeping to answer status. Trace output can follow log locations recorded by the driver without a process-to-process connection or a second verdict path.

### Shared state and storage

`record` gives shared durable concepts one definition: `Repository`, `Change`, `Revision`, `Job`, `Attempt`, `Resource`, `PublicationAction`, and `PullRequest`, with explicit IDs and revision references. It also contains shared values required by those records, such as immutable build inputs and outcome evidence. These names identify different lifetimes; they do not imply a package or state machine for every struct.

Keep package-specific requests and intermediate results with their owning capability. `record` must not become a miscellaneous collection of services, provider SDK types, terminal strings, or duplicate versions of existing records. Backend encoding, constraints, and schema migration belong in `state/sqlite`; domain invariants remain with their owning packages.

`state` defines persistence contracts, record-specific reads/writes, bounded queries, transaction semantics, and backend-independent errors. Every view or transaction is bound to one repository. `state/sqlite` implements those contracts with SQLite transactions, constraints, indexes, and migrations. SQL and the Go database driver stay private to that package. Neither package invokes Git or decides workflow policy. There is no whole-state serialization API or interface per table.

Claims stay in the same transaction as the state they protect. `workflow` owns request intake and progression decisions, including atomic cross-record updates; capabilities return results for it to record. An independently replaceable lock backend must not authorize workflow writes. If a concrete executor later needs an external-resource lock, define a separate small contract then. No generic lock service or new filesystem lock implementation is needed for the state migration.

`git` provides repository mechanics: immutable object creation, ref transactions, materialized snapshots, source reads, and guarded pushes. Temporary materializations and prepared objects remain distinct from branch integration. Git operations and database writes cannot commit atomically together. Workflow inspects interrupted preparation and records completion or a need for attention. Storage does not pin source objects; operations validate the particular inputs they need.

### One workflow owner

The driver is the running process; `workflow` implements request intake and the execution engine, and `proc` supplies current-process residency and persistent-loop lifetime. The state store carries requests and recorded progress between processes. There is no separate `driver` package. Action invocations and `dockhand start` run the same Dockhand-specific engine.

Scheduling, claims, transitions, retries, and recovery stay together because they jointly determine whether work may advance. Pure verification judgment and publication policy remain in their capability packages. This engine manages Dockhand jobs; it is not a general-purpose workflow framework.

`workflow` accepts requests, binds their inputs, records jobs, claims ready work, invokes capabilities, records outcomes, and advances the requested destination. It owns scheduling, retry decisions, cancellation, resource retention, cleanup, and publication continuation. Workflow supplies candidate-selection criteria to bounded state queries; handlers recheck eligibility and acquire claims transactionally. Neither a synchronous command nor an adapter gets its own alternative progression loop.

Use explicit handlers and typed results for the few kinds of work. A cycle claims a bounded action in a short state transaction, performs the work after commit, and records its result only if the claim and relevant revision remain current. Uncertain provider or forge effects go through reconciliation before another submission. Resource-release obligations survive job completion.

Observation and judgment remain separate within the capability packages. The driver consumes their results and commits the state transition. It does not reimplement version comparisons, Tcl semantics, failure classification, or publication eligibility.

### Preparing and understanding source

`macports` resolves ports and selectors and binds evaluation to the complete source context: snapshot, tree resources, selected subport, variants, and platform. It exposes evaluated metadata, before-and-after snapshots, and dependency information. Preparation, verification preflight, and publication context checks all use this boundary. They must not each reconstruct a target from a directory string.

`tcl` supplies the proven process/RPC and syntax machinery. MacPorts remains the semantic authority. Reuse focused source editing and Tcl syntax code where it holds up independently; it does not need to be redesigned to fit a driver.

`upstream` collects release evidence, assesses eligible versions, and resolves explicit version/reference requests. It keeps the requested spelling, MacPorts version, and upstream tag distinct. Prefix inference uses the current port's source convention supplied through bound MacPorts metadata and confirms the candidate against upstream evidence. It returns structured update-available, current, and unknown results. It has no dependency on job submission, a state writer, or branch creation. Phase-one automatic and explicit bumps use it; phase-two `outdated` exposes discovery directly.

`prepare` turns a requested source transformation into proposed tree-wide edits, per-file preconditions, commit intent, and fidelity evidence. It coordinates MacPorts evaluation, upstream discovery when needed, downloads/checksums, and any required auxiliary-file generation. Begin with version bump, revision bump, and checksum refresh in one package. Extract specialized download or vendoring helpers when porting working implementations makes a useful boundary clear.

For current-checkout verification, `git` supplies immutable working-tree snapshot capture, `macports` resolves targets against that snapshot, and `workflow` records its provenance and any contribution association. Standalone verification must not reserve a branch for one port. Edited ports and verification targets remain separate; `verify` judges evidence applicability across identical trees and matching build inputs, and `publish` requires committed source. These are approved boundaries for the next source-selection work, not implemented behavior.

Preparation can create temporary files and Git objects, but returns the result for driver adoption. `--diff` calls the same preparation capability and renders its proposed changes without creating a job or moving tracked refs. Phase-two rebase and amend become additional preparation actions returning the same kind of result. Explicit user edits can be captured as immutable input without needing to rediscover an upstream version.

### Verification and dependent work

`verify` owns the provider contract, immutable build specifications, verification coverage planning, evidence interpretation, and pure verdict logic. The driver owns attempt state transitions. The provider contract covers capabilities, submission, observation, reconciliation by durable submission identity, cancellation, and release; it returns serializable handles that another process can use.

Distinguish admitted, temporarily at capacity, unsupported, and submission-uncertain outcomes. A preliminary capacity check is advisory: actual admission must coordinate at the provider's resource scope, including other repositories sharing the same host. State transactions cover record mutations; provider admission still needs provider-owned capacity coordination.

Dependent work has three distinct outputs: proposed revision-bump edits, a target/configuration coverage plan, and the dependency information needed to build each target. Keep dependency discovery in `macports`, dependent selection/impact analysis in `verify`, source edits in `prepare`, and readiness scheduling in `workflow`. The driver records any required review decision before applying additional edits. These can initially be files within the existing packages.

Use an isolated verification unit per target/configuration by default, scheduling them sequentially if capacity requires. Conflicting dependents do not disappear from the revision-bump set. Resource ownership is per concrete attempt, not just per change and platform. Provider builds resolve actual dependency closures; a dependency outside the edited cohort is still relevant build context.

Planned follow-up targets may refer to predecessor work, but freeze concrete artifact identities before submitting an attempt. Artifact reuse and baseline comparisons can be added later without changing the distinction between a coverage plan and an immutable attempt. Do not create a generic graph engine or another package resolver.

`verify/tart` implements the provider contract and owns VM-specific admission, provisioning, guest execution, evidence extraction, and resource operations. It does not publish PRs or mutate workflow records. Its lifecycle logic, native Tart/launchd adapter, exact-source archive mechanics, and guest runner remain inside `verify/tart`. A narrow `state.ProviderStore` interface supplies pool-scoped transactions on the same SQLite backend; provider-owned executions and workflow adoption are separate records. Per-submission OS locks serialize external VM mutations, while SQLite owns shared reservations and durable closure. Large logs and build artifacts can stay outside the database, with stable references returned to the driver and explicit retention responsibilities.

### Publication and later PR awareness

`publish` gathers forge facts, evaluates publication policy, renders desired PR content, and determines the concrete next action. It also supplies reconciliation logic for uncertain actions. Its action executor uses Git and forge capabilities; the driver records intent before calling it and records the confirmed result afterward.

Desired revision, expected remote head, PR title/body, and observed forge state are separate facts. A matching SHA does not establish that metadata is current, and an existing PR does not prove an attempted edit succeeded. Preserve a stable PR association across repeated publication jobs.

`forge/github` supplies concrete release and PR observations and mutations. Define small interfaces where `upstream` and `publish` consume these capabilities, and wire the adapter in `app`. Neither capability imports the concrete GitHub adapter. Phase-two PR monitoring adds observation methods consumed by the same driver; it does not need a second publication controller. Remote CI evidence remains distinct from local verification.

## Dependency rules

- `record` has no dependency on CLI, proc, workflow, storage, or concrete integrations.
- `state` depends on shared records and standard-library contracts, not Git, SQLite, or workflow policy.
- `state/sqlite` depends on `state`, `record`, and the selected SQLite driver. It does not import workflow or Git.
- `workflow` depends on `state` and capability APIs. Capabilities do not depend back on the engine or write its records.
- `proc` supplies current-process residency around `workflow.Engine`. Requests and observations pass through state; `proc` does not judge evidence or choose the next business action.
- `app` wires concrete integrations, including SQLite, and owns their lifetime. Define other interfaces at actual external or test boundaries.

This fits in the existing Go module. The persistence migration needs a Go SQLite driver, selected during implementation; it does not need a database server, ORM, broker, plugin loader, or general scheduling library.

## A single execution path

For `bump jq --publish --wait`:

1. The CLI calls the shared workflow submission API, which transactionally records a queued job and returns its ID. The invoking process runs targeted workflow cycles; it or an already-running persistent driver claims the recorded work through state transactions.
2. The driver prepares and integrates a revision using Git preconditions, then records it and the next work in state. Interrupted integration is inspected before retrying; this is not one atomic Git/database transaction.
3. Verification constructs the build question; the driver claims an attempt and asks the provider to admit it. Capacity pressure leaves it waiting, with the CLI still attached.
4. Later cycles observe the provider, judge and record evidence, and schedule required dependent work. Failure in one independent target does not prevent others progressing.
5. Publication evaluates the recorded evidence and fresh forge facts. The driver records and executes the required publication actions, reconciling uncertain outcomes.
6. The CLI observes completion and exits. Any remaining resource cleanup stays recorded for a running persistent driver or a later workflow cycle.

Without `--wait`, the same workflow runs and the CLI leaves after the applicable admission milestone. If no persistent `dockhand start` process is running, later settlement, publication, and cleanup await another driver cycle. Already-submitted provider work may continue independently; the command must not promise automatic workflow continuation after its process exits. `verify`, `publish`, resumed jobs, and later `amend` all join this path at the appropriate stage. `status` reads a projection; `outdated` reads discovery results; neither takes ownership of a job by observing it.

## What to carry forward from v1

| Existing component or finding | Treatment in v2 |
| --- | --- |
| Tcl shell/RPC/syntax and bound MacPorts handles | Reuse implementation as appropriate; preserve complete source context and design tests separately. |
| Upstream observations and pure judgment | Retain the separation; expose it independently of bump execution. |
| `statestore` plus derived `ledger` notes | Preserve durable identities and atomic workflow decisions behind `state`; replace Git persistence with SQLite and omit notes export. |
| Preparation spread through planning, preparation, change, and CLI composition | Consolidate the public preparation boundary and tree-wide edit representation. |
| `app`, `run`, `lease`, and cycle paths | Move workflow transitions into `workflow`; keep capability logic and provider mechanisms separate. |
| Verification verdicts and provider adapters | Reuse isolated judgment and mechanics, adapting their contracts to explicit attempt and resource identity. |
| Publication authorization and reconciliation | Preserve the separation and carry forward regression cases for remote-head and metadata confirmation. |
| Shared-guest cohort assumptions | Replace scheduling assumptions with per-target coverage and attempt-owned resources. |

Earlier reviews are evidence of failure modes, not a claim that every finding remains unfixed in current v1. For the initial groundwork, import neither v1 prose comments nor tests. Record reused code and newly authored components in the activity report. Later, design focused tests around the new boundaries and important workflow cases rather than copying the old suite.

## Next implementation slice

The state migration is complete: intake, status, cancellation, and the single-target cycle use `state` and `state/sqlite`, with repository registration, scoped queries, and `--db`. Cross-process tests cover concurrent writers, abandoned transactions, and competing driver claims. The [implementation report](activity/2026-09-12-sqlite-state.md) and [measurements](performance/2026-09-12-sqlite-state.md) describe the result.

Explicit branch binding and native MacPorts evaluation are now implemented. `workflow.BindVerification` uses Git snapshot mechanics and a bound `macports.Tree`; `Submit` atomically records the selected branch revision and job. Resolution initially supports a snapshot-relative port directory/Portfile or a unique directory name, plus an explicit subport. It does not use an installed PortIndex to resolve names in a different source snapshot. The [source-binding report](activity/2026-09-12-source-binding.md) describes the API, tests, and current limits.

Real Tart execution is now implemented through this path. CLI verification submission/observation and current-process residency are now connected. Next, add preparation and publication through the same engine. Their tables and queries arrive with their executors; the initial database does not need publication, review, discovery, or dependent-graph tables. Existing domain distinctions remain available for those features.

Committed changes to a tracked branch can now become another immutable revision when explicitly bound and submitted. The [approved source-selection design](cli-design.md#approved-source-selection-and-human-edits) now specifies working-tree verification, explicit committed-branch selection, inferred contribution scope, and standalone verification without an exclusive branch association. Implement those boundaries before building preparation on the current one-branch/one-target restriction. Optional bump versions and upstream-prefix inference are also approved; their execution remains part of the preparation work. Discovery, rebase, amend, and PR monitoring extend the existing packages without a second execution path.

## Groundwork status

The package skeleton, selected Tcl/source-editing helpers, SQLite state backend, workflow intake/status, and first verification cycle are present. `--db` defaults to `$HOME/.dockhand/state.db`; status reads existing state without creating a database or registering repositories. Shared worktrees and separate clones are covered by registration tests. The Git ledger, source-pin manager, and filesystem lock package have been removed.

The first cycle handles single-target planning, capacity waiting, admission, observation, cancellation, submission reconciliation, and independent cleanup. It selects bounded batches through indexed queries and rechecks claims transactionally. Unsupported executors produce needs-attention outcomes. Preparation, publication, dependent scheduling, evidence reuse, review controls, and other CLI action handlers remain unfinished. Verification, job-ID wait/cancel, and current-process start are implemented. Tart execution now has fault-injection tests and an opt-in real build that submits in one process and settles the same run in another. See the [Tart execution report](activity/2026-09-12-tart-execution.md).

The [groundwork](activity/2026-09-10-groundwork.md), [ledger](activity/2026-09-10-ledger.md), [startup configuration](activity/2026-09-10-config-directory.md), [Cobra integration](activity/2026-09-10-cobra.md), [lockfile simplification](activity/2026-09-11-lockfile.md), [workflow intake/status](activity/2026-09-11-workflow-intake.md), and [verification cycle](activity/2026-09-11-verification-cycle.md) reports describe provenance, implementation, and validation. The [lock-directory report](activity/2026-09-12-lock-directory.md) records the former configuration; the [state design report](activity/2026-09-12-state-design.md) and [implementation report](activity/2026-09-12-sqlite-state.md) document its replacement. Earlier reports remain historical records. The [behavioral test report](activity/2026-09-11-behavior-tests.md) describes the first permanent tests for `workflow`, `ledger`, and `tcl/syntax`. All tests were authored for v2; none were copied from v1.

The [performance pass](activity/2026-09-12-performance-pass.md) batches Git source validation and avoids transactions for known ineligible cycle work. Its measurements motivate replacing whole-ledger writes with affected-row updates; the reports and benchmark inputs remain useful comparison evidence.

### CLI execution and attachment

`app.Services.BindVerification` composes native platform discovery, Tart configuration capture, and workflow source binding. Cobra parses input, submits through the engine, renders recorded progress, and selects an attachment milestone. `workflow.Reached` evaluates admission/completion from records. `proc.Manager` owns the cancellable loop shared by targeted attachment and resident execution; it owns no workflow state and creates no driver discovery records. Resident passes use indexed cycle queries and do not build full historical status snapshots.

`verify.LogReader` is an optional read-only diagnostic interface. Tart reads bounded guest log ranges while running and retained host logs after collection. CLI tracing keeps offsets, drains final logs, and writes to stderr without participating in workflow bookkeeping. The [CLI execution report](activity/2026-09-12-cli-execution.md) records scope and validation.
