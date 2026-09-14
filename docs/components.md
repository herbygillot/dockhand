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
    credential/          # Device authorization and secret-store contracts
      keychain/          # macOS Keychain implementation
    record/              # Shared durable records, identities, and value types
    state/               # Repository-scoped persistence and transaction contracts
      sqlite/            # SQLite storage, connections, and schema migrations
    workflow/            # Request acceptance and all workflow advancement
    prepare/             # Source transformations and edit-fidelity checks
    upstream/            # Release discovery and version assessment
    verify/              # Build specifications, coverage plans, verdicts
      tart/              # Concrete VM verification provider and image coordination
        provision/       # Tart base-image construction and validation
    publish/             # Publication policy, desired state, reconciliation
    macports/            # Bound source contexts, evaluation, dependencies
    tcl/                 # Tcl process/RPC support and source syntax tools
    text/                # Byte spans and source-preserving edits
    git/                 # Git objects, refs, snapshots, guarded remote pushes
    forge/               # Remote facts, repository access, and PR write inputs
      github/            # GitHub naming, URLs, HTTP, tags/releases, PR adapter
  docs/
  go.mod
```

The initial internal files can be straightforward: `workflow/submit.go`, `cycle.go`, `prepare.go`, `verification.go`, `publish.go`, `cleanup.go`, and `status.go`. They share the same workflow engine and transaction rules. A separate Go package is justified by a useful dependency boundary, not by every lifecycle noun or CLI verb.

## Responsibilities

### Entry points and process lifetime

`cmd/dockhand` contains executable startup and exit handling. `app` resolves configuration, discovers the selected repository, constructs dependencies, and owns their lifetime. Cobra passes global `--db PATH` through `app.Config.DBPath`, defaulting to `$HOME/.dockhand/state.db`. Open state lazily: writable operations initialize it and register a repository; status uses read-only lookup and does not create missing state. Help, completion generation, and previews do not open it. `app` injects a `state.Store`; it must not acquire a second workflow sequence as commands grow. No config-directory concept or lock-directory flag remains in this design.

`cli` uses Cobra for the command tree, flag parsing, argument validation, generated help, and shell completion. It parses commands into typed requests and renders typed results. It owns human output, JSON output, exit-code mapping, and the choice to observe admission or completion. It submits requests through the shared workflow API rather than writing record shapes itself; it never settles an attempt or performs driver bookkeeping. Domain packages do not print terminal messages or decide exit codes.

`proc` manages residency and persistent execution within the current `dockhand` process. Change commands run targeted workflow cycles in their invocation; `dockhand start` explicitly runs persistent mode for the selected repository. No separate executable, executable-path discovery, or automatic child driver launch is needed. The database's repository ID comes from canonical Git common-directory registration. Linked worktrees share an entry; separate clones remain distinct. Database selection is independent of the checkout.

`credential` defines the small device-authorization and secret-store boundaries used by repository-independent login. `credential/keychain` stores the native GitHub token through macOS Keychain without placing it in process arguments. `forge/github` implements the OAuth device endpoint and identity check. `app` wires those mechanics, while `cli` owns the browser prompt and human or JSON result. Login neither constructs repository services nor opens state.

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

The job's recorded phase selects the responsible handler directly. Intake chooses the initial phase, and a handler advances it only in the transaction that records the checkpoint completing its own phase. Preparation, verification, and publication may remain focused files in this package; sharing one engine does not require each cycle to infer which file owns a job from the incidental presence of other records.

Use explicit handlers and typed results for the few kinds of work. A cycle claims a bounded action in a short state transaction, performs the work after commit, and records its result only if the claim and relevant revision remain current. Uncertain provider or forge effects go through reconciliation before another submission. Resource-release obligations survive job completion.

Observation and judgment remain separate within the capability packages. The driver consumes their results and commits the state transition. It does not reimplement version comparisons, Tcl semantics, failure classification, or publication eligibility.

### Preparing and understanding source

`macports` resolves ports and selectors and binds evaluation to the complete source context: snapshot, tree resources, selected subport, variants, and platform. It exposes evaluated metadata, before-and-after snapshots, and dependency information. Preparation, verification preflight, and publication context checks all use this boundary. They must not each reconstruct a target from a directory string.

`tcl` supplies the proven process/RPC and syntax machinery. MacPorts remains the semantic authority. Reuse focused source editing and Tcl syntax code where it holds up independently; it does not need to be redesigned to fit a driver.

`upstream` collects release evidence, assesses eligible versions, and resolves explicit version/reference requests. It keeps the requested spelling, MacPorts version, and upstream tag distinct. Prefix inference uses the current port's source convention supplied through bound MacPorts metadata and confirms the candidate against upstream evidence. It returns structured update-available, current, and unknown results. It has no dependency on job submission, a state writer, or branch creation. Phase-one automatic and explicit bumps use it; phase-two `outdated` exposes discovery directly.

`prepare` turns a requested source transformation into proposed tree-wide edits, per-file preconditions, commit intent, and fidelity evidence. It coordinates MacPorts evaluation, upstream discovery when needed, downloads/checksums, and any required auxiliary-file generation. Begin with version bump, revision bump, and checksum refresh in one package. Extract specialized download or vendoring helpers when porting working implementations makes a useful boundary clear.

For current-checkout verification, `git` supplies immutable working-tree snapshot capture, `macports` resolves targets against that snapshot, and `workflow` records its provenance and any contribution association. Standalone verification must not reserve a branch for one port. Edited ports and verification targets remain separate; `verify` judges evidence applicability across identical trees and matching build inputs, and `publish` requires committed source. Working-tree capture and evidence reuse are implemented. Standalone verification and separate edited/verification targets are implemented.

Preparation can create temporary files and Git objects, but returns the result for driver adoption. `--diff` calls the same preparation capability and renders its proposed changes without creating a job or moving tracked refs. Phase-two rebase and amend become additional preparation actions returning the same kind of result. Explicit user edits can be captured as immutable input without needing to rediscover an upstream version.

### Verification and dependent work

`verify` owns the provider contract, immutable build specifications, verification coverage planning, evidence interpretation, and pure verdict logic. The driver owns attempt state transitions. The provider contract covers capabilities, submission, observation, reconciliation by durable submission identity, cancellation, and release; it returns serializable handles that another process can use.

Distinguish admitted, temporarily at capacity, unsupported, and submission-uncertain outcomes. A preliminary capacity check is advisory: actual admission must coordinate at the provider's resource scope, including other repositories sharing the same host. State transactions cover record mutations; provider admission still needs provider-owned capacity coordination.

Dependent work has three distinct outputs: proposed revision-bump edits, a target/configuration coverage plan, and the dependency information needed to build each target. Keep dependency discovery in `macports`, dependent selection/impact analysis in `verify`, source edits in `prepare`, and readiness scheduling in `workflow`. The driver records any required review decision before applying additional edits. These can initially be files within the existing packages.

Use an isolated verification unit per target/configuration by default, scheduling them sequentially if capacity requires. Conflicting dependents do not disappear from the revision-bump set. Resource ownership is per concrete attempt, not just per change and platform. Provider builds resolve actual dependency closures; a dependency outside the edited cohort is still relevant build context.

Planned follow-up targets may refer to predecessor work, but freeze concrete artifact identities before submitting an attempt. Artifact reuse and baseline comparisons can be added later without changing the distinction between a coverage plan and an immutable attempt. Do not create a generic graph engine or another package resolver.

`verify/tart` implements the provider contract and owns VM-specific admission, guest execution, evidence extraction, resource operations, and base-image coordination. `verify/tart/provision` owns the setup recipe and native construction mechanics without joining the attempt lifecycle. Neither publishes PRs or mutates workflow records. The provider lifecycle, native Tart/launchd adapter, exact-source archive mechanics, and guest runner remain inside `verify/tart`. A narrow `state.ProviderStore` interface supplies pool-scoped transactions on the same SQLite backend; provider-owned executions and workflow adoption are separate records. Per-submission OS locks serialize external VM mutations, while SQLite owns shared reservations and durable closure. Per-image read/write locks coordinate verification with base replacement, and a setup lock serializes provisioning independently of database selection. Large logs and build artifacts can stay outside the database, with stable references returned to the driver and explicit retention responsibilities. `verify.ArtifactPruner` is an optional provider capability for idempotent deletion of released diagnostics under the same operation lock. `workflow.Collect` owns age selection and records confirmed pruning; it reuses ordinary claimed cleanup for VM release. No second job progression loop or general garbage-collector package is introduced.

### Publication and later PR awareness

`publish` gathers forge facts, evaluates publication policy, renders desired PR content, and determines the concrete next action. It also supplies reconciliation logic for uncertain actions. Its action executor uses Git and forge capabilities; the driver records intent before calling it and records the confirmed result afterward.

Desired revision, expected remote head, PR title/body, and observed forge state are separate facts. A matching SHA does not establish that metadata is current, and an existing PR does not prove an attempted edit succeeded. Preserve a stable PR association across repeated publication jobs.

`forge` defines transient remote tag/release and PR observations, PR write inputs, repository access, and remote lookup/completeness errors. `forge/github` validates GitHub repository names and owns public URLs, HTTP configuration and tag peeling, delegating pagination to `go-github`; its PR adapter implements lookup, observation, creation, and metadata updates. `upstream.RepositoryReader` binds one `forge.Repository`, which supplies all tag/release observations and public URLs used for that selection. `publish.Forge` consumes PR operations using the shared inputs and observations. `app` wires the concrete client; neither capability imports it, and the adapter imports neither capability. Phase-two PR monitoring adds observation methods consumed by the same driver; it does not need a second publication controller. Remote CI evidence remains distinct from local verification.

## Dependency rules

- `record` has no dependency on CLI, proc, workflow, storage, or concrete integrations.
- `state` depends on shared records and standard-library contracts, not Git, SQLite, or workflow policy.
- `state/sqlite` depends on `state`, `record`, and the selected SQLite driver. It does not import workflow or Git.
- `credential` defines authorization and storage contracts without depending on a concrete forge, Keychain, CLI, or workflow. `credential/keychain` implements only its storage contract.
- `forge` defines remote facts and access contracts using shared records and the standard library. It imports no capability or concrete adapter.
- `forge/github` depends on `credential`, `forge`, `record`, OAuth transport, and Git validation mechanics; it imports neither `upstream`, `publish`, nor `macports`.
- `upstream` and `publish` consume forge contracts and observations. MacPorts source conventions and version/publication policy stay in the consuming capability.
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

## Implementation roadmap

Current priorities, unresolved product decisions, and deferred work are tracked in the [roadmap](roadmap.md). This component map defines package responsibilities and dependency boundaries rather than maintaining a second work queue.

Branch-based wait/cancel and continuous integration are implemented. Authentication discovery, preflight, native login, and image-free selection of matching recorded verification are implemented. Explicit and environment credentials, Dockhand's Keychain credential, and the active `gh` account are resolved for publication; standalone and combined publication binders check identity before acceptance, and the driver repeats the check immediately before each remote effect. Device login stays outside repository state and uses a registered OAuth client ID supplied by the build, environment, or command line.

Tart image setup is implemented as a state-independent application operation. It validates existing images in disposable clones and provisions a missing or explicitly rebuilt native base from a pinned guest-agent asset and an explicit MacPorts version. An explicit Xcode archive or directory creates a separate full-Xcode profile with exact version validation. Conventional release-based names allow verification to select the prepared base image when `--image` is omitted. A golden copy and candidate-first replacement order provide bounded recovery, while per-image external locks coordinate setup with concurrent verification even across different database selections.

### Implemented foundations

The state migration is complete: intake, status, cancellation, and the single-target cycle use `state` and `state/sqlite`, with repository registration, scoped queries, and `--db`. Cross-process tests cover concurrent writers, abandoned transactions, and competing driver claims. The [implementation report](activity/2026-09-12-sqlite-state.md) and [measurements](performance/2026-09-12-sqlite-state.md) describe the result.

Explicit branch binding and native MacPorts evaluation are now implemented. `workflow.BindVerification` uses Git snapshot mechanics and a bound `macports.Tree`; `Submit` atomically records the job and, for an existing tracked contribution, its selected revision. Standalone verification creates no contribution. Resolution initially supports a snapshot-relative port directory/Portfile or a unique directory name, plus an explicit subport. It does not use an installed PortIndex to resolve names in a different source snapshot. The [source-binding report](activity/2026-09-12-source-binding.md) describes the API, tests, and current limits.

Real Tart execution is now implemented through this path. CLI verification submission/observation and current-process residency are now connected. Revision preparation and its preview now exercise the source-transformation boundary. Durable revision-bump preparation now connects to that same engine. Explicit version bumps now use this engine too. Automatic version discovery is implemented through the same release checkpoint. Standalone publication now adds its scoped action and PR tables; review, discovery, and dependent-graph tables remain deferred. Existing domain distinctions remain available for those features.

Committed changes to a tracked branch can now become another immutable revision when explicitly bound and submitted. The [approved source-selection design](cli-design.md#approved-source-selection-and-human-edits) now specifies working-tree verification, explicit committed-branch selection, inferred contribution scope, and standalone verification without an exclusive branch association. Standalone verification and separate edited/verification targets are implemented. Working-tree capture and single-target inference from a tracked contribution are implemented. `workflow/verification_target.go` owns recorded-target selection and scope checks; `git.ChangedPaths` supplies immutable path differences, shared with capture and publication. MacPorts resolves and evaluates the selected target, and the driver receives explicit intent. Explicit bump versions and upstream-prefix inference are now implemented for supported GitHub-backed sources. Discovery, rebase, amend, and PR monitoring extend the existing packages without a second execution path.

## Groundwork status

The package skeleton, selected Tcl/source-editing helpers, SQLite state backend, workflow intake/status, and first verification cycle are present. `--db` defaults to `$HOME/.dockhand/state.db`; status reads existing state without creating a database or registering repositories. Shared worktrees and separate clones are covered by registration tests. The Git ledger, source-pin manager, and filesystem lock package have been removed.

The first cycle handles single-target planning, capacity waiting, admission, observation, cancellation, submission reconciliation, and independent cleanup. It selects bounded batches through indexed queries and rechecks claims transactionally. Unsupported executors produce needs-attention outcomes. Durable version- and revision-bump preparation are implemented. Standalone and combined bump/publication are implemented; dependent scheduling, review controls, and the remaining CLI action handlers are unfinished. Automatic selection now supports supported GitHub conventions. Verification, fixed job- or contribution-selected wait/cancel, and current-process start are implemented. Tart execution now has fault-injection tests and an opt-in real build that submits in one process and settles the same run in another. See the [Tart execution report](activity/2026-09-12-tart-execution.md).

The [groundwork](activity/2026-09-10-groundwork.md), [ledger](activity/2026-09-10-ledger.md), [startup configuration](activity/2026-09-10-config-directory.md), [Cobra integration](activity/2026-09-10-cobra.md), [lockfile simplification](activity/2026-09-11-lockfile.md), [workflow intake/status](activity/2026-09-11-workflow-intake.md), and [verification cycle](activity/2026-09-11-verification-cycle.md) reports describe provenance, implementation, and validation. The [lock-directory report](activity/2026-09-12-lock-directory.md) records the former configuration; the [state design report](activity/2026-09-12-state-design.md) and [implementation report](activity/2026-09-12-sqlite-state.md) document its replacement. Earlier reports remain historical records. The [behavioral test report](activity/2026-09-11-behavior-tests.md) describes the first permanent tests for `workflow`, `ledger`, and `tcl/syntax`. All tests were authored for v2; none were copied from v1.

The [performance pass](activity/2026-09-12-performance-pass.md) batches Git source validation and avoids transactions for known ineligible cycle work. Its measurements motivate replacing whole-ledger writes with affected-row updates; the reports and benchmark inputs remain useful comparison evidence.

### CLI execution and attachment

`app.Services.BindVerification` composes native platform discovery, Tart configuration capture, and workflow source binding. Cobra parses input, submits through the engine, renders recorded progress, and selects an attachment milestone. `workflow.Reached` evaluates admission/completion from records. `proc.Manager` owns the cancellable loop shared by targeted attachment and resident execution; it owns no workflow state and creates no driver discovery records. Resident passes use indexed cycle queries and do not build full historical status snapshots.

The Tart guest runs lint, build, declared tests when enabled, and installation in that order. Build, test, and install enable debug output, which goes to the same retained log streamed by `--trace`; lint remains quiet. Build failures stop the sequence and record a separate build step. Dependency binaries remain enabled unless `--from-source` is selected.

The provider prepares a PortIndex before staging a frozen source. `--prefix` selects the host MacPorts `portindex`; its executable digest and the mirror URL participate in frozen provider settings. Following MacPorts CI, a cold cache downloads a platform index, reconciles the recent base history, and retains the result by immutable base tree. Candidate trees reuse that index and re-evaluate only port directories changed from the base. Download failure or changes under `_resources` cause a full pass because shared PortGroups can change unrelated entries. Candidate indexes remain temporary and the guest consumes the staged index without occupying build time with a full-tree index pass.

`verify.LogReader` is an optional read-only diagnostic interface. Tart reads bounded guest log ranges while running and retained host logs after collection. CLI tracing keeps offsets, drains final logs, and writes to stderr without participating in workflow bookkeeping. The [CLI execution report](activity/2026-09-12-cli-execution.md) records scope and validation.

### Preparation previews and version-selection groundwork

`prepare.Service` now owns immutable source materialization, target resolution, precise revision edits, and before/after fidelity checks. It returns proposed tree identity, target, file edits, commit intent, and observations for later workflow adoption. `git.FileEdit` is the shared physical edit shape; the unused preparation-specific duplicate was removed. `git.EditTree` checks file preconditions and writes only changed blobs and ancestor trees, without touching refs or the user's index.

`app.PreviewPreparation` constructs only the capabilities a preview needs. `bump-revision --diff` renders the real result without state or a provider. Literal subport selection and sibling fidelity work through complete MacPorts evaluation. The driver uses the same capability for job progression, guarded branch integration, and verification continuation. Working-tree input is supported by verification; bump previews continue selecting committed source and do not adopt a branch association.

`upstream.MatchRelease` separates deterministic explicit-version/tag selection from evidence collection. `upstream.Resolve` now collects exact tag observations through a bound `forge.Repository`; `forge/github` supplies its HTTP implementation, including annotated-tag peeling. `upstream.Check` validates the recorded source during preparation. `upstream.DiscoverPort` now selects an eligible stable numeric version from GitHub release/tag catalogs. The [groundwork report](activity/2026-09-13-bump-groundwork.md) and [explicit-version report](activity/2026-09-13-explicit-version-bumps.md) record the two slices and their provenance.

GitHub-backed `go.setup` now shares the version editor and preparation pipeline with `github.setup`. `prepare` owns the version argument edit and checks the evaluated `go.version` change. `macports` inspects native fetch targets and reports `fetch.archive_compatible`: standard archive fetching and the structurally recognized Go toolchain pre-check are supported. Raw hook bodies stay within the adapter’s RPC/decoding boundary. Unknown hooks or changed hook structure require another preparer; no arbitrary hooks run during preview. MacPorts executes the normal hooks in the verification VM. See the [Go-port preparation report](activity/2026-09-13-go-port-preparation.md).

### Durable preparation and integration

`workflow/preparation_source.go` binds source and accepted choices. `preparation.go` claims immutable preparation and checkpoints a candidate commit. `integration.go` reconciles the destination ref and records the contribution/result revision. The consumer-owned `SourcePreparer` interface exposes the existing preparation capability; it does not add another workflow abstraction. `verification.go` uses the same attempt lifecycle for standalone inputs and prepared results.

`git/branch.go` provides Git identity lookup and branch-specific exclusion. Its descriptor reaches short Git subprocesses through the existing repository command boundary. This lock protects ref effects and recovery, while SQLite continues to authorize state transitions. Schema 3 adds job claim/checkpoint fields and permits standalone plans/attempts without contribution revisions; it introduces no table or package. The existing `x/sys` dependency is now direct.

`app.Services.BindPreparation` captures source, platform, author, and verification configuration. Shared CLI build flags feed `verify`, `bump`, and `bump-revision`; attachment and progress rendering remain shared. The [revision-driver report](activity/2026-09-13-revision-driver.md) records behavior, recovery limits, and validation.


`prepare/source.go` owns shared source loading and candidate evaluation. Version editing, checksum source edits, and HTTP stream hashing remain focused files in `prepare`; they do not need new packages yet. Version and revision transformations share the final metadata comparator. The workflow's small `ReleaseResolver` interface exposes the preparer's release-resolution capability separately so the driver can persist that result before preparation. `record.Release` describes the durable selected source; `forge.Release` describes a remote release observation, and `upstream.Candidate` adds a possible Portfile version without redefining those facts.

Schema 4 adds one immutable-once-set JSON column to `jobs` for the selected release. It introduces no table, state interface, lock, or generic coordination abstraction. `golang.org/x/crypto` provides the legacy RIPEMD-160 checksum MacPorts Portfiles use. The existing HTTP client injection points support independent network tests. GitHub discovery can use public access; publication resolves an explicit token, environment token, Dockhand Keychain entry, or `gh` credential. Credentials are not persisted in the release checkpoint or workflow database.

## Automatic version selection

Automatic and explicit bumps share `upstream.Resolve`, the release checkpoint, preparation, integration, and verification. `upstream/latest.go` owns eligibility and current/update/unknown assessment. The bound `forge.Repository` lists releases and tags; `forge/github` consumes `go-github` iterators directly with default pagination options. GitHub source/PortGroup conventions are isolated in `upstream/github.go`; the version-selection files contain no GitHub URL construction. `macports/versions.go` and its Tcl script implement `VersionSelector`, applying the port's evaluated list-encoded regex and native `vercmp` ordering. The evaluator preserves raw Tcl option values; list consumers interpret them at their own boundary.

An automatic `record.Release` additionally retains `CurrentVersion` and `NoUpdate`. The workflow atomically records a current observation and completes the job without invoking preparation or verification. Existing JSON storage accommodates these optional fields without a schema migration. There are no new packages or dependencies. See the [implementation report](activity/2026-09-13-automatic-version-selection.md) for scope, tests, and live checks.

## Working-tree verification

`git/worktree.go` captures raw bytes for paths tracked by the selected worktree's index, including staged additions and deletions. It preserves index flags in the real index while independently reading working files, computes Git blob identities, writes changed blobs, and assembles a tree with a disposable index. SHA-1 and SHA-256 repositories are supported. Two content passes plus index/HEAD checks detect ordinary concurrent changes; sparse entries and conflicts are refused.

`workflow.BindVerification` chooses current-checkout capture when `Branch` is empty and committed binding otherwise. Both inputs share isolated materialization, MacPorts target resolution/evaluation, acceptance, and the existing verification cycle. A dirty source has a tree without a commit; `record.Checkout` separately records branch, observed HEAD, and modified-file count in immutable job options. Tracked contributions retain revision association; standalone and detached verification create no contribution. SQLite's existing options JSON stores this provenance, with no schema migration.

`verify.PlanSingle` and Tart accept tree-only sources. Tart still checks commit/tree agreement whenever a commit is supplied, and always materializes the accepted tree. Guest input, durable provider identity, recovery, and cancellation remain on the existing path. Evidence reuse after later commits and tracked-target inference are implemented; standalone publication can use matching evidence. See the [working-tree report](activity/2026-09-13-working-tree-verification.md).


## Verification reuse

`verify/reuse.go` compares complete build inputs, checks partial accepted build requirements, and judges recorded evidence without querying storage or calling a provider. `workflow/reuse.go` selects from bounded original-attempt candidates and records the decision during initial planning. A prepared job without an image may select an exact prior configuration only when its provider, platform, test, and source-build requirements match. A newer matching negative result prevents reuse of an older pass. `state.Reader.VerificationCandidates` supplies repository-scoped history through indexed SQLite queries; schema 5 adds the job's original-attempt reference and diagnostic detail. The requirements fit in the existing job options JSON and add no migration, cache package, or duplicated evidence record.

Tart records a verifier digest alongside the existing image digest and frozen settings. The digest includes guest code, the guest-command descriptor-isolation wrapper, its launch description, and an explicit host-protocol version marker. Changes to host execution semantics that are not represented in those inputs must advance that marker. Missing legacy identities disable reuse. The CLI exposes `verify --fresh` and projects the original attempt separately from a job's own executions. See the [reuse report](activity/2026-09-13-verification-reuse.md).


## Forge and upstream boundaries

`forge/github.Client.Repository(name)` validates and binds a name without HTTP. The returned repository owns URL construction and exact-tag/release/catalog access. This replaces independently supplied tag and release readers, which could accidentally address different adapters. Repository strings do not need to be revalidated or supplied on every request. HTTP API configuration remains on the client; the API origin and public GitHub web origin are separate concepts. The adapter uses the pure-Go `go-github` library for typed API operations, request construction, authentication, API headers, JSON, and pagination metadata. SDK types stay inside `forge/github`; callers continue to use `forge` contracts. The adapter retains credential discovery, authenticated-user checks, observation validation, same-origin read redirects, disabled write redirects, and publication-outcome classification. It initializes public and authenticated SDK clients lazily and shares them across bound repositories without persisting credentials.

Repository clone URLs and PR/release web URLs come from SDK response fields. Clone locations are checked against the intended repository before Git uses them; PR identity comes from its repository and number, without reconstructing its web URL. `TagsPageURL` and `TagLivecheckURL` are the remaining public URL builders: they describe links used by MacPorts livecheck regexes. SDK `TagsURL` is an API endpoint, while `TarballURL` and `GetArchiveLink` identify downloads; neither is a substitute for that browser-page convention. Source downloads still follow evaluated MacPorts `master_sites` and `distfiles`. See the [URL and resource audit](activity/2026-09-13-github-resource-urls.md).

`upstream/github.go` interprets evaluated GitHub PortGroup fields and recognizes the supported livecheck convention. `TagPattern` supplies one implementation of prefix/suffix mapping for explicit selection, automatic filtering, source checks, and pattern validation. Generic policy results and durable selected releases retain their existing meaning. Tcl option collection remains in `macports`; source edits to `github.setup` and fidelity checks remain in `prepare`. Those operations concern Portfiles, not GitHub's API.

Missing-ref classification happens only in exact-tag lookup. `go-github` preserves structured API errors, so a 404 while peeling an annotation or reading a catalog cannot become evidence that the requested tag is absent. The SDK owns API defaults and pagination. The adapter adds no record/page counts, body-size bounds, page-size overrides, tag-depth caps, or operation timeouts. Discovery and tag lookup use the caller's context; workflow call deadlines and cancellation remain in effect. See the [refactor report](activity/2026-09-13-forge-upstream-boundaries.md).

## Implemented publication boundary

`workflow.BindPublication` selects a committed source and its applicable verification. For an untracked branch, `publish/source.go` derives its single parent and changed port directory; a tree/Portfile lookup supplies the latest verified target and configuration. Acceptance creates the contribution and revision with the publication job in one transaction. `publish.Plan` owns contribution scope, destination selection, content, and remote preconditions; `publish` also owns PR observation validation and metadata policy. `forge/github` owns GitHub protocol and URL rules. `git` owns remote inspection, literal-ref pushes with explicit expected values, ancestry checks, and the local operation lock. `state` exposes only publication and PR methods required by this path; SQLite migration 6 supplies their storage.

The shared cycle claims the job, takes a lock for its forge/head-repository/head-branch, rechecks ownership and cancellation, and records a checkpoint before each external effect. No state transaction spans Git or HTTP. PR uncertainty selects observation-only recovery; it never automatically reissues a create or update. Status joins the action and retained PR association. See the [publication report](activity/2026-09-13-publication.md) for boundaries and tests.


## Verification performance and timing

`verify/tart/image.go` owns prepared-image content identity and metadata checks. `state.ImageCache`, included in `ProviderStore`, keeps the storage boundary independent of SQLite. The backend adds a provider/path cache in schema 7; it does not introduce a new package, generic lock service, or alternate source of verification evidence.

`workflow/timing.go` owns operation deadlines. Each preparation, provider, publication, or cleanup action uses the corresponding budget for both its call and its expiring claim. `ObserveInterval` determines the persisted next observation for a running build, while `RetryDelay` governs failures, capacity waiting, uncertain outcomes, and cancellation. Other drivers respect the recorded deadline. CLI status/log refresh has its own cadence and reads recorded progress without forcing a provider observation.

CLI completion reports the verification outcome directly. Reuse explanations remain separate progress/status details, and detach output names the driver's remaining settlement and cleanup responsibility. See the [activity report](activity/2026-09-13-verification-performance.md) and [measurements](performance/2026-09-13-verification-overhead.md).

## Combined preparation and publication

`publish.Destination` resolves immutable destination choices at intake; `publish.PlanTo` combines that destination with a verified prepared revision and current remote preconditions. Standalone `publish.Plan` composes both. `workflow/publication_plan.go` owns the durable transition between verification and publication; the existing publication executor owns push, PR writes, confirmation, and recovery for both command paths. No new package, dependency, schema, or child job is introduced.

`record.JobSpec.PublishTo` holds a combined job's destination. `record.PublicationSpec` remains the complete external-action intent, populated only once the prepared revision passes verification. Workflow checks the action against the accepted destination and prepared revision before persisting it and before claiming it for execution; SQLite preserves the resulting intent and its relationships. Verification and publication read the result source without replacing the accepted source. See the [combined publication report](activity/2026-09-13-combined-publication.md).
