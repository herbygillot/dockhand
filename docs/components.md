# Initial component structure

This is a proposed implementation map for the [architecture](architecture.md), [principles](principles.md), and [CLI design](cli-design.md). Those documents and the v1 reviews are sufficient to establish these boundaries. The first working workflow should validate the exact APIs before they become fixed conventions.

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
    record/               # Shared durable records, identities, and value types
    ledger/              # Git transactions, record encoding, derived notes
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

`cmd/dockhand` contains executable startup and exit handling. `app` constructs dependencies, resolves configuration, and exposes setup services. Cobra owns the global `--lockfile` / `-L` flag and its `$HOME/.dockhand/ledger.lock` default. The selected path passes through `app.Config.Lockfile` to `ledger.Options.Lockfile`; `ledger.New` creates the file and any missing parent directories without acquiring the writer lock. There is no config-directory setting or startup directory creation. `app` must stay wiring and configuration; it must not acquire a second workflow sequence as commands grow. A preview or read-only status request should not require an available VM provider.

`cli` uses Cobra for the command tree, flag parsing, argument validation, generated help, and shell completion. It parses commands into typed requests and renders typed results. It owns human output, JSON output, exit-code mapping, and the choice to observe admission or completion. It submits requests through the shared workflow API rather than writing record shapes itself; it never settles an attempt or performs driver bookkeeping. Domain packages do not print terminal messages or decide exit codes.

`proc` manages driver residency and persistent execution within the current `dockhand` process. Change commands run targeted workflow cycles in their own invocation; `dockhand start` explicitly runs persistent driver mode. No driver executable setting, executable-path discovery, or automatic child driver launch is needed. Repository identity resolves through the Git common directory. Ledger writer coordination uses the configured lockfile; linked worktree invocations share it when they select the same lockfile.

The ledger is the request handoff and progress channel. The CLI calls `workflow.Submit` in its own process to validate and transactionally persist a queued job, then runs targeted workflow cycles in that invocation. A resident driver or targeted cycle reads eligible work from the ledger and claims it transactionally. There is no socket or separate request transport. Action invocations and explicit persistent mode execute the same `workflow.Engine`.

Successful durable submission establishes acceptance and returns the job ID; it does not mean a driver has claimed the job or a provider has admitted a build. Request IDs allow an interrupted caller to find its recorded submission without duplicating it. If the invocation ends before finishing its work, the request remains recorded and recoverable without claiming admission or completion. Cancellation and review requests use the same workflow-owned intake path; the driver applies their consequences.

The CLI observes progress by reading the ledger through a shared read-only status projection. While waiting, the invocation can also run targeted cycles through the workflow engine; those cycles retain all bookkeeping responsibility. `wait` repeatedly reads the selected jobs until their requested milestone is recorded; `status` works even when no driver is alive. Preserve observation timestamps and do not poll a provider or run bookkeeping to answer status. Trace output can follow log locations recorded by the driver without a process-to-process connection or a second verdict path.

### Shared state and storage

`record` gives shared durable concepts one definition: `Change`, `Revision`, `Job`, `Attempt`, `Resource`, `PublicationAction`, and `PullRequest`, with explicit IDs and revision references. It also contains shared values required by those records, such as immutable build inputs and outcome evidence. These names identify different lifetimes; they do not imply a package or state machine for every struct.

Keep package-specific requests and intermediate results with their owning capability. `record` must not become a miscellaneous collection of services, provider SDK types, terminal strings, or duplicate versions of existing records. Serialization and schema checks belong in `ledger`.

`ledger` owns the authoritative state ref, short transactions, locking at the supplied path, expected-ref checks, source pins, and derived Git notes. Notes export is part of this component; do not expose another authoritative note writer. Business decisions stay out of storage. `workflow` owns request intake and progression writes, including cross-record updates. Intake can run in the CLI process; after submission, drivers own progression and bookkeeping. Capability packages return results for the workflow engine to record.

`git` provides concrete repository mechanics: immutable object creation, ref transactions, materialized snapshots, source reads, and guarded push operations. Temporary materializations and unreferenced prepared objects are distinct from adopting a revision into a tracked branch. The driver commits that adoption and its records through the ledger transaction. Future rebase and amend preparation must preserve this distinction.

### One workflow owner

The driver is the running process; `workflow` implements request intake and the execution engine, and `proc` supplies current-process residency and persistent-loop lifetime. The ledger carries requests and recorded progress between processes. There is no separate `driver` package. Action invocations and `dockhand start` run the same Dockhand-specific engine.

Scheduling, claims, transitions, retries, and recovery stay together because they jointly determine whether work may advance. Pure verification judgment and publication policy remain in their capability packages. This engine manages Dockhand jobs; it is not a general-purpose workflow framework.

`workflow` accepts requests, binds their inputs, records jobs, claims ready work, invokes capabilities, records outcomes, and advances the requested destination. It owns scheduling, retry decisions, cancellation, resource retention, cleanup, and publication continuation. Neither a synchronous command nor an adapter gets its own alternative progression loop.

Use explicit handlers and typed results for the few kinds of work. A cycle claims a bounded action under the ledger lock, performs the work outside that lock, and records its result only if the claim and relevant revision remain current. Uncertain provider or forge effects go through reconciliation before another submission. Resource-release obligations survive job completion.

Observation and judgment remain separate within the capability packages. The driver consumes their results and commits the state transition. It does not reimplement version comparisons, Tcl semantics, failure classification, or publication eligibility.

### Preparing and understanding source

`macports` resolves ports and selectors and binds evaluation to the complete source context: snapshot, tree resources, selected subport, variants, and platform. It exposes evaluated metadata, before-and-after snapshots, and dependency information. Preparation, verification preflight, and publication context checks all use this boundary. They must not each reconstruct a target from a directory string.

`tcl` supplies the proven process/RPC and syntax machinery. MacPorts remains the semantic authority. Reuse focused source editing and Tcl syntax code where it holds up independently; it does not need to be redesigned to fit a driver.

`upstream` collects release evidence and assesses eligible versions. It returns structured update-available, current, and unknown results. It has no dependency on job submission, a ledger writer, or branch creation. Phase-one automatic bumps use it; phase-two `outdated` exposes the same service directly.

`prepare` turns a requested source transformation into proposed tree-wide edits, per-file preconditions, commit intent, and fidelity evidence. It coordinates MacPorts evaluation, upstream discovery when needed, downloads/checksums, and any required auxiliary-file generation. Begin with version bump, revision bump, and checksum refresh in one package. Extract specialized download or vendoring helpers when porting working implementations makes a useful boundary clear.

Preparation can create temporary files and Git objects, but returns the result for driver adoption. `--diff` calls the same preparation capability and renders its proposed changes without creating a job or moving tracked refs. Phase-two rebase and amend become additional preparation actions returning the same kind of result. Explicit user edits can be captured as immutable input without needing to rediscover an upstream version.

### Verification and dependent work

`verify` owns the provider contract, immutable build specifications, verification coverage planning, evidence interpretation, and pure verdict logic. The driver owns attempt state transitions. The provider contract covers capabilities, submission, observation, reconciliation by durable submission identity, cancellation, and release; it returns serializable handles that another process can use.

Distinguish admitted, temporarily at capacity, unsupported, and submission-uncertain outcomes. A preliminary capacity check is advisory: actual admission must coordinate at the provider's resource scope, including other repositories sharing the same host. The ledger lock covers record mutations; provider admission still needs provider-owned capacity coordination.

Dependent work has three distinct outputs: proposed revision-bump edits, a target/configuration coverage plan, and the dependency information needed to build each target. Keep dependency discovery in `macports`, dependent selection/impact analysis in `verify`, source edits in `prepare`, and readiness scheduling in `workflow`. The driver records any required review decision before applying additional edits. These can initially be files within the existing packages.

Use an isolated verification unit per target/configuration by default, scheduling them sequentially if capacity requires. Conflicting dependents do not disappear from the revision-bump set. Resource ownership is per concrete attempt, not just per change and platform. Provider builds resolve actual dependency closures; a dependency outside the edited cohort is still relevant build context.

Planned follow-up targets may refer to predecessor work, but freeze concrete artifact identities before submitting an attempt. Artifact reuse and baseline comparisons can be added later without changing the distinction between a coverage plan and an immutable attempt. Do not create a generic graph engine or another package resolver.

`verify/tart` implements the provider contract and owns VM-specific admission, provisioning, guest execution, evidence extraction, and resource operations. It does not publish PRs or mutate the ledger. Large logs and build artifacts can stay outside Git, with stable references returned to the driver and explicit retention responsibilities.

### Publication and later PR awareness

`publish` gathers forge facts, evaluates publication policy, renders desired PR content, and determines the concrete next action. It also supplies reconciliation logic for uncertain actions. Its action executor uses Git and forge capabilities; the driver records intent before calling it and records the confirmed result afterward.

Desired revision, expected remote head, PR title/body, and observed forge state are separate facts. A matching SHA does not establish that metadata is current, and an existing PR does not prove an attempted edit succeeded. Preserve a stable PR association across repeated publication jobs.

`forge/github` supplies concrete release and PR observations and mutations. Define small interfaces where `upstream` and `publish` consume these capabilities, and wire the adapter in `app`. Neither capability imports the concrete GitHub adapter. Phase-two PR monitoring adds observation methods consumed by the same driver; it does not need a second publication controller. Remote CI evidence remains distinct from local verification.

## Dependency rules

- `record` has no dependency on CLI, proc, workflow, storage, or concrete integrations.
- `ledger` depends on `record` and Git mechanics, not preparation, verification, or publication policy.
- `workflow` depends on the ledger and capability APIs. Capabilities do not depend back on the workflow engine or write its records.
- `proc` handles current-process residency and persistent-loop lifetime around `workflow.Engine`. Requests and observations pass through the ledger; `proc` does not judge evidence or choose the next business action.
- `app` is the place concrete integrations are wired. Define interfaces at actual external or test boundaries; concrete structs and functions are sufficient elsewhere.

This permits action invocations and persistent driver mode to share behavior without introducing interfaces around every function. All of this fits in the existing Go module. The architecture itself requires no new external framework, database, broker, plugin loader, or general scheduling library.

## A single execution path

For `bump jq --publish --wait`:

1. The CLI calls the shared workflow submission API, which transactionally records a queued job and returns its ID. The invoking process runs targeted workflow cycles; it or an already-running persistent driver claims the recorded work through the ledger.
2. The driver invokes preparation, then atomically adopts the resulting revision and records the next required work.
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
| `statestore` plus derived `ledger` notes | Keep Git durability and transactions behind one new ledger component. |
| Preparation spread through planning, preparation, change, and CLI composition | Consolidate the public preparation boundary and tree-wide edit representation. |
| `app`, `run`, `lease`, and cycle paths | Move workflow transitions into `workflow`; keep capability logic and provider mechanisms separate. |
| Verification verdicts and provider adapters | Reuse isolated judgment and mechanics, adapting their contracts to explicit attempt and resource identity. |
| Publication authorization and reconciliation | Preserve the separation and carry forward regression cases for remote-head and metadata confirmation. |
| Shared-guest cohort assumptions | Replace scheduling assumptions with per-target coverage and attempt-owned resources. |

Earlier reviews are evidence of failure modes, not a claim that every finding remains unfixed in current v1. For the initial groundwork, import neither v1 prose comments nor tests. Record reused code and newly authored components in the activity report. Later, design focused tests around the new boundaries and important workflow cases rather than copying the old suite.

## First implementation slice

The shared records, ledger, request intake, snapshot status projection, and first verification cycle are implemented. The cycle verifies one resolved target against an existing committed revision using the recorded build configuration and an injected provider. Temporary scripted-provider checks exercise capacity, admission, completion, cancellation, process death, competing drivers, and independent cleanup. Connect CLI submission and observation through the ledger, add current-process persistent execution through `proc`, and confirm that changing attachment does not change execution. Verify that a request survives submission before any driver starts, and that concurrent cycles cannot claim the same action.

Next add real Tart verification, source preparation, and publication through the same driver. Use temporary Git repositories, representative Portfile fixtures, and scripted forge responses to cover source context, stale claims, uncertain submissions, and revision/metadata reconciliation. A dependent build scenario should prove that resource ownership and partial coverage are not limited to one build per change.

Then add command handlers and capabilities incrementally. Phase-two discovery, rebase, amend, and ongoing PR monitoring should extend the existing packages. Exact record fields, request-intake validation, claim deadlines, edit-selection syntax, and provider-specific recovery limits can be resolved in these slices; they do not prevent choosing the component structure now.

## Groundwork status

The package skeleton, selected Tcl/source-editing helpers, and ledger persistence are present. The ledger implements snapshot reads, bounded writer locking, guarded transactions, structural document validation, and source pins. Lockfile selection, ledger initialization, and construction of the service objects are wired. Cobra supplies the phase-one command tree, global/local flags, argument and flag-conflict checks, help, and completion. Workflow intake validates and transactionally accepts queued requests with idempotent receipts; snapshot status reporting is wired to `dockhand status`, including JSON output.

The first driver cycle, cancellation intake/application, single-target planning, and evidence judgment are present. Attempt and resource claims fence ledger writes; provider reconciliation must close an absent submission identity before a fresh identity can be tried. The cycle does not implement source preparation, publication, dependent scheduling, or evidence reuse. Tart operations, derived notes, review controls, persistent residency, and CLI action handlers remain explicit stubs. Provider calls were exercised through a temporary scripted adapter; no real VM build has run.

The [groundwork](activity/2026-09-10-groundwork.md), [ledger](activity/2026-09-10-ledger.md), [startup configuration](activity/2026-09-10-config-directory.md), [Cobra integration](activity/2026-09-10-cobra.md), [lockfile simplification](activity/2026-09-11-lockfile.md), [workflow intake/status](activity/2026-09-11-workflow-intake.md), and [verification cycle](activity/2026-09-11-verification-cycle.md) reports describe provenance, implementation, and validation. The lockfile report supersedes the earlier config-directory behavior. No test suite has been copied or added.
