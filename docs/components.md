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
    subprocess/          # One external command with shared capture and error conventions
    progress/            # Optional transient operation observations
    credential/          # Device authorization and secret-store contracts
      keychain/          # macOS Keychain implementation
    github/              # Shared GitHub credentials, SDK client, and transport
    fetch/               # Bounded HTTP transfers with caller-owned content policy
    filelock/            # Context-aware locks for shared external resources
    record/              # Shared durable records, identities, and value types
    state/               # Repository-scoped persistence and transaction contracts
      sqlite/            # SQLite storage, connections, and schema migrations
    workflow/            # Request acceptance and all workflow advancement
      preparation/       # Git snapshot lifetime and final edited-tree storage
      policy/            # Read-only evidence, coverage, and publication questions
    assess/              # Preparation capability reports, without jobs or downloads
    outdated/            # Read-only committed-port update scans
    upstream/            # Release discovery and version assessment
    verify/              # Build specifications, coverage plans, verdicts
      staging/           # Immutable indexed source archives
      tart/              # Concrete Tart verification provider
      github/            # Fork Actions verification and SDK adapter
    atomicfile/          # Durable replacement of small local files
    archive/             # Tar and zip member walks without host extraction
    macos/               # OS/toolchain facts, operations, and launchd plist rendering
    tart/                # Shared local Tart commands, runtime paths, images, and coordination
      host/              # VM lifecycle, launchd, foreground boot, and guest transport
      provision/         # Tart base-image construction and validation
    publish/             # Publication policy, desired state, reconciliation
    macports/            # Bound source contexts and evaluated observations
      selection/         # Source-bound indexed port/subport names and validation
      eval/              # Native Tcl evaluation and MacPorts runtime compatibility
      installation/      # MacPorts installation and observed installation facts
      fidelity/          # Before-and-after evaluation comparison reports
      portedit/          # Evaluator-driven source edits in a disposable workspace
      portfile/          # Tcl literal candidates and precise source edits
      version/           # Version spellings: validation, tag patterns, stability
      patchcheck/        # Declared patch files checked against a candidate source
      dependents/        # Frozen-source downstream coverage discovery
      survey/            # Shared committed-source lifetime and port selection
      source/            # Evaluated PortGroup source conventions
      portindex/         # Frozen-source PortIndex construction and caching
    tcl/                 # Tcl process/RPC support and source syntax tools
    text/                # Byte spans and source-preserving edits
    git/                 # Git objects, refs, snapshots, guarded remote pushes
      changeset/         # Captured sources, explicit-base deltas, commit facts
    forge/               # Remote facts, repository access, and PR write inputs
      github/            # GitHub tags/releases and PR adapter
      gitlab/            # GitLab tag observations
  docs/
  go.mod
```

The initial internal files can be straightforward: `workflow/submit.go`, `cycle.go`, `prepare.go`, `verification.go`, `publish.go`, `cleanup.go`, and `status.go`. They share the same workflow engine and transaction rules. A separate Go package is justified by a useful dependency boundary, not by every lifecycle noun or CLI verb.

## Responsibilities

### Entry points and process lifetime

`cmd/dockhand` contains executable startup and exit handling. `app` resolves configuration, discovers the selected repository, constructs dependencies, and owns their lifetime. Cobra passes global `--db PATH` through `app.Config.DBPath`, defaulting to `$HOME/.dockhand/state.db`. Open state lazily: writable operations initialize it and register a repository; status uses read-only lookup and does not create missing state. Help, completion generation, and previews do not open it. `app` injects a `state.Store`; it must not acquire a second workflow sequence as commands grow. No config-directory concept or lock-directory flag remains in this design.

`cli` uses Cobra for the command tree, flag parsing, argument validation, generated help, and shell completion. It parses commands into typed requests and renders typed results. It owns human output, JSON output, exit-code mapping, and the choice to observe admission or completion. It submits requests through the shared workflow API rather than writing record shapes itself; it never settles an attempt or performs driver bookkeeping. Domain packages do not print terminal messages or decide exit codes.

`proc` manages residency and persistent execution within the current `dockhand` process. Change commands run targeted workflow cycles in their invocation; `dockhand start` explicitly runs persistent mode for the selected repository. No separate executable, executable-path discovery, or automatic child driver launch is needed. The database's repository ID comes from canonical Git common-directory registration. Linked worktrees share an entry; separate clones remain distinct. Database selection is independent of the checkout.

`credential` defines the small device-authorization and secret-store boundaries used by repository-independent login. `credential/keychain` stores the native GitHub token through macOS Keychain without placing it in process arguments. `github` implements the OAuth device endpoint and identity check. `app` wires those mechanics, while `cli` owns the browser prompt and human or JSON result. Login neither constructs repository services nor opens state.

The state store is the request handoff and progress channel. The CLI calls `workflow.Submit` in its own process to validate and transactionally persist a queued job, then runs targeted workflow cycles in that invocation. A resident driver or targeted cycle reads eligible work from the state store and claims it transactionally. There is no socket or separate request transport. Action invocations and explicit persistent mode execute the same `workflow.Engine`.

Successful durable submission establishes acceptance and returns the job ID; it does not mean a driver has claimed the job or a provider has admitted a build. Request IDs allow an interrupted caller to find its recorded submission without duplicating it. If the invocation ends before finishing its work, the request remains recorded and recoverable without claiming admission or completion. Cancellation and review requests use the same workflow-owned intake path; the driver applies their consequences.

The CLI observes progress by reading state through a shared read-only status projection. While waiting, the invocation can also run targeted cycles through the workflow engine; those cycles retain all bookkeeping responsibility. `wait` repeatedly reads the selected jobs until their requested milestone is recorded; `status` works even when no driver is alive. Preserve observation timestamps and do not poll a provider or run bookkeeping to answer status. Trace output can follow log locations recorded by the driver without a process-to-process connection or a second verdict path.

### Shared state and storage

`record` gives shared durable concepts one definition: `Repository`, `Change`, `Revision`, `Job`, `Attempt`, `Resource`, `PublicationAction`, and `PullRequest`, with explicit IDs and revision references. It also contains shared values required by those records, such as immutable build inputs and outcome evidence. These names identify different lifetimes; they do not imply a package or state machine for every struct.

Keep package-specific requests and intermediate results with their owning capability. `record` must not become a miscellaneous collection of services, provider SDK types, terminal strings, or duplicate versions of existing records. Backend encoding, constraints, and schema migration belong in `state/sqlite`; domain invariants remain with their owning packages.

`workflow/policy` answers read-only questions over a state reader: which recorded evidence applies to a build question, whether evidence covers a publication's cohort, and whether a publication action still matches its job; both intake and drivers ask it. `subprocess` runs one external command with the conventions every tool runner shares, a bounded wait after cancellation, captured output returned even on failure, per-stream limits, and one error naming the tool, command, and stderr, while each caller keeps its own environment policy. `state` defines persistence contracts, record-specific reads/writes, bounded queries, transaction semantics, and backend-independent errors. Every view or transaction is bound to one repository. `state/sqlite` implements those contracts with SQLite transactions, constraints, indexes, and migrations. SQL and the Go database driver stay private to that package. Neither package invokes Git or decides workflow policy. There is no whole-state serialization API or interface per table.

Claims stay in the same transaction as the state they protect. `workflow` owns request intake and progression decisions, including atomic cross-record updates; capabilities return results for it to record. An independently replaceable lock backend must not authorize workflow writes. If a concrete executor later needs an external-resource lock, define a separate small contract then. No generic lock service or new filesystem lock implementation is needed for the state migration.

`git` provides repository mechanics: immutable object creation, ref transactions, materialized snapshots, source reads, and guarded pushes. `git/changeset` composes those primitives into captured branch or checkout snapshots, explicit-base deltas, and single-commit publication facts. It does not interpret changed paths as MacPorts targets or decide workflow policy. Temporary materializations and prepared objects remain distinct from branch integration. Git operations and database writes cannot commit atomically together. Workflow inspects interrupted preparation and records completion or a need for attention. Storage does not pin source objects; operations validate the particular inputs they need.

### One workflow owner

The driver is the running process; `workflow` implements request intake and the execution engine, and `proc` supplies current-process residency and persistent-loop lifetime. The state store carries requests and recorded progress between processes. There is no separate `driver` package. Action invocations and `dockhand start` run the same Dockhand-specific engine.

Scheduling, claims, transitions, retries, and recovery stay together because they jointly determine whether work may advance. Pure verification judgment and publication policy remain in their capability packages. This engine manages Dockhand jobs; it is not a general-purpose workflow framework.

`workflow` accepts requests, binds their inputs, records jobs, claims ready work, invokes capabilities, records outcomes, and advances the requested destination. It owns scheduling, retry decisions, cancellation, resource retention, cleanup, and publication continuation. Workflow supplies candidate-selection criteria to bounded state queries; handlers recheck eligibility and acquire claims transactionally. Neither a synchronous command nor an adapter gets its own alternative progression loop.

The job's recorded phase selects the responsible handler directly. Intake chooses the initial phase, and a handler advances it only in the transaction that records the checkpoint completing its own phase. Preparation, verification, and publication may remain focused files in this package; sharing one engine does not require each cycle to infer which file owns a job from the incidental presence of other records.

Use explicit handlers and typed results for the few kinds of work. A cycle claims a bounded action in a short state transaction, performs the work after commit, and records its result only if the claim and relevant revision remain current. Uncertain provider or forge effects go through reconciliation before another submission. Resource-release obligations survive job completion.

Observation and judgment remain separate within the capability packages. The driver consumes their results and commits the state transition. It does not reimplement version comparisons, Tcl semantics, failure classification, or publication eligibility.

### Preparing and understanding source

The next preparation expansion follows [the bump-planner design](bump-planner.md). The native evaluator extraction is implemented; declaration provenance, artifact ownership, and alternate-context planning remain staged work.

`macports` defines evaluation contracts and binds the complete source context: snapshot, tree resources, selected subport, variants, and platform. It exposes evaluated metadata, before-and-after snapshots, and dependency information. `macports/eval` implements these contracts through native Tcl: runtime startup, compatibility checks, selector resolution, metadata decoding, fetch observations, and version comparisons. Its `Session` keeps one interpreter for repeated evaluations of one tree, satisfying `macports.BatchReader`; each evaluation still opens the port afresh, so rewritten contents are observed. Application wiring selects the concrete evaluator; preparation and workflow consume the shared contracts. The evaluator reports observations and does not choose edits or decide which releases to preserve. `macports/source` interprets evaluated GitHub and GitLab PortGroup fields into a forge instance, repository, tag convention, catalog choice, and the text shape consumed by the port's livecheck regex. Preparation, verification preflight, and publication context checks all use these boundaries. They must not each reconstruct a target from a directory string or reinterpret PortGroup defaults.

`tcl` supplies the proven process/RPC and syntax machinery. MacPorts remains the semantic authority. Reuse focused source editing and Tcl syntax code where it holds up independently; it does not need to be redesigned to fit a driver.

`macports/dependency` owns Go/Cargo declaration parsing, source-manifest reading, optional helper discovery/execution, and validation of generated blocks. `macports/portedit` composes these mechanisms with frozen-tree editing, archive hashing, and MacPorts reevaluation. Helper output is data: only recognized literal dependency declarations are incorporated, never a generated Portfile as executable Tcl. This introduces no workflow or state layer. `archive` walks tar and zip members for both the manifest reader and `macports/patchcheck`, which extracts only the files a port's patch files name and runs the system patch command in check mode; preparation records whether each declared patch still applies, the workflow creates the branch but does not start verification while a patch is rejected, and nothing refuses the version.

`upstream` collects release evidence, assesses eligible versions, and resolves explicit version/reference requests. It keeps the requested spelling, MacPorts version, and upstream tag distinct. Prefix inference uses the current port's source convention supplied through bound MacPorts metadata and confirms the candidate against upstream evidence. It returns structured update-available, current, and unknown results. `macports/version` is a pure leaf that owns version spellings: validation, the tag pattern that maps a Portfile version to its upstream tag, and classification as stable, prerelease, or unknown. Ordering stays with MacPorts through the evaluator. Every resolved release records its classification and whether it takes the port out of stable, which reporting states and nothing refuses. It has no dependency on job submission, a state writer, or branch creation. `Service.Bind` composes a source-specific `VersionProbe` with the catalogs without mutating the shared service. This assessment needs no bump request or edit-fidelity approval; preparation separately checks whether the selected release can be edited safely. Phase-one automatic and explicit bumps use it; phase-two `outdated` exposes discovery directly.

`macports/fidelity` compares MacPorts evaluations before and after an edit and reports whether only the intended metadata changed; it knows nothing about editing, downloads, or workspaces. `macports/portedit` edits an exclusively owned disposable source workspace and returns Portfile edits, commit intent, and fidelity reports. The workspace owns the write-evaluate-restore cycle and its paths; an archive store owns downloads and decides whether bytes are kept for dependency generators; `Result.commitEdit` is the one place an edit becomes a commit intent. It coordinates MacPorts evaluation, archive checksums, and dependency regeneration. Its `VersionProbe` binds metadata and calculated-version evaluation to one caller-owned disposable workspace; it has no upstream catalog dependency. `EvaluateVersions` evaluates many candidate source versions through one batch when the reader supports it, with the same per-candidate evaluation and Portfile restoration. It imports no Git package and creates no branches, objects, jobs, or publications. `macports/portfile` identifies literal inputs, including assignments and command-substitution arguments, and applies precise replacements without implementing Tcl semantics.

`workflow/preparation` materializes the immutable input, binds its version probe to upstream discovery, passes the disposable workspace to the editor, and translates returned edits into Git file preconditions. It checks upstream source identity before and after editing. It stores only the final candidate tree and reevaluates that tree before returning it to workflow. Intermediate probe snapshots have no immutable source identity. The workflow engine still owns candidate commits, branch integration, and state transitions.

For current-checkout verification, `git/changeset` binds the low-level Git capture into an immutable source snapshot, `macports` resolves targets against that snapshot, and `workflow` records its provenance and any contribution association. Standalone verification must not reserve a branch for one port. Edited ports and verification targets remain separate; `verify` judges evidence applicability across identical trees and matching build inputs, and `publish` requires committed source. Working-tree capture and evidence reuse are implemented. Standalone verification and separate edited/verification targets are implemented.

Preparation can create temporary files and Git objects, but returns the result for driver adoption. `--diff` calls the same preparation capability and renders its proposed changes without creating a job or moving tracked refs. Phase-two rebase and amend become additional preparation actions returning the same kind of result. Explicit user edits can be captured as immutable input without needing to rediscover an upstream version.

### Verification and dependent work

`verify` owns the provider contract, immutable build specifications, verification coverage planning, evidence interpretation, and pure verdict logic. The driver owns attempt state transitions. The provider contract covers capabilities, submission, observation, reconciliation by durable submission identity, cancellation, and release; it returns serializable handles that another process can use.

Distinguish admitted, temporarily at capacity, unsupported, and submission-uncertain outcomes. A preliminary capacity check is advisory: actual admission must coordinate at the provider's resource scope, including other repositories sharing the same host. State transactions cover record mutations; provider admission still needs provider-owned capacity coordination.

Dependent work has three distinct outputs: proposed revision-bump edits, a target/configuration coverage plan, and the dependency information needed to build each target. Keep dependency discovery in `macports`, dependent selection/impact analysis in `verify`, source edits in `macports/portedit`, and readiness scheduling in `workflow`. `macports/portindex` now supplies exact-index lookup, direct reverse dependencies, and transitive forward closure without making coverage decisions. The driver records any required review decision before applying additional edits. These can initially be files within the existing packages.

Use an isolated verification unit per target/configuration by default, scheduling them sequentially if capacity requires. Conflicting dependents do not disappear from the revision-bump set. Resource ownership is per concrete attempt, not just per change and platform. Provider builds resolve actual dependency closures; a dependency outside the edited cohort is still relevant build context.

Planned follow-up targets may refer to predecessor work, but freeze concrete artifact identities before submitting an attempt. Artifact reuse and baseline comparisons can be added later without changing the distinction between a coverage plan and an immutable attempt. Do not create a generic graph engine or another package resolver.

`macos` owns Darwin/product-version/release-name mappings, Xcode archive selection, and developer-tool observation and installation through an explicitly supplied command target. It imports no Tart, verification, or state packages and never chooses a host implicitly. Observation does not install tools or compile files; compiler smoke tests and installation are separate operations. Tart retains supported-image policy, disk-layout assumptions, and guest transport. Both provisioning and verification consume the shared developer-tool inspection.

`tart` owns mechanics shared by image construction and verification: local command execution, conventional release profiles, image manifests, and per-image coordination. `tart/provision` owns the setup recipe and native construction mechanics without joining the attempt lifecycle. `verify/tart` implements the provider contract and owns VM-specific admission, guest execution, image capability inspection, evidence extraction, and resource operations. Neither publishes PRs nor mutates workflow records. The provider lifecycle and guest build runner remain inside `verify/tart`; shared launchd-backed VM control is in `tart/host`. `verify/staging` materializes the accepted tree, stages its exact index, checks the selected target, and atomically packages the source with caller-supplied payload files. Tart owns the guest payload and prepares the source archive before capacity reservation, then rechecks capacity transactionally before creating a guest. A narrow `state.ProviderStore` interface supplies pool-scoped transactions and provider-wide image observations on the same SQLite backend; provider-owned executions and workflow adoption are separate records. An uncached image is inspected inside the admitted disposable clone before source staging, so inspection uses ordinary capacity and never changes the source image. The immutable image digest keys cached capabilities across repositories, while terminal evidence retains the observed capability identity for reuse decisions. `filelock` supplies context-aware advisory locks for external files and resources. Per-submission locks serialize external VM mutations, while SQLite owns shared reservations and durable closure. Per-image read/write locks coordinate verification with base replacement, and a setup lock serializes provisioning independently of database selection. Large logs and build artifacts can stay outside the database, with stable references returned to the driver and explicit retention responsibilities. `verify.ArtifactPruner` is an optional provider capability for idempotent deletion of released diagnostics under the same operation lock. `workflow.Collect` owns age selection and records confirmed pruning; it reuses ordinary claimed cleanup for VM release. No second job progression loop or general garbage-collector package is introduced.

### Publication and later PR awareness

`publish` gathers forge facts, evaluates publication policy, renders desired PR content, and determines the concrete next action. It also supplies reconciliation logic for uncertain actions. Its action executor uses Git and forge capabilities; the driver records intent before calling it and records the confirmed result afterward.

Desired revision, expected remote head, PR title/body, and observed forge state are separate facts. A matching SHA does not establish that metadata is current, and an existing PR does not prove an attempted edit succeeded. Preserve a stable PR association across repeated publication jobs.

`forge` defines transient remote tag/release and PR observations, PR write inputs, repository access, and remote lookup/completeness errors. `forge/github` supplies GitHub tag/release observations through `go-github`; its PR adapter implements lookup, observation, creation, metadata updates, and `Inspect`, which reads mergeability, the latest review per reviewer, check runs, and commit statuses for a PR head into `record.PullRequestStatus`. `forge.PullRequestInspector` is optional: the workflow inspects an open PR on `refresh` when the forge implements it and stores the status with the PR record. `forge/gitlab` supplies GitLab tag observations through GitLab's Go SDK. `upstream.Catalog` binds an interpreted instance and repository to one `forge.Repository`; release access is an optional capability because the GitLab path currently uses tags only. `publish.Forge` consumes PR operations using the shared inputs and observations. `app` wires concrete clients; neither capability imports it, and the adapters import neither capability. PR inspection is consumed by the same lifecycle path as PR state; it does not need a second publication controller. Remote CI evidence remains distinct from local verification.

## Dependency rules

- `record` has no dependency on CLI, proc, workflow, storage, or concrete integrations.
- `state` depends on shared records and standard-library contracts, not Git, SQLite, or workflow policy.
- `state/sqlite` depends on `state`, `record`, and the selected SQLite driver. It does not import workflow or Git.
- `credential` defines authorization and storage contracts without depending on a concrete forge, Keychain, CLI, or workflow. `credential/keychain` implements only its storage contract.
- `filelock` depends only on the standard library and coordinates external resources without authorizing state changes.
- `tart` owns runtime resolution, CLI invocation, image metadata, naming, and coordination. `tart/host` consumes it for concrete VM lifecycle and guest transport, with macOS launchd rendering and atomic file replacement. `tart/provision` owns recipes, SSH bootstrap, readiness deadlines, and validation. None imports verification, workflow, or state.
- `atomicfile` depends only on the standard library. It replaces small files durably; callers retain naming, cache policy, and lifecycle ownership.
- `forge` defines remote facts and access contracts using shared records and the standard library. It imports no capability or concrete adapter.
- `github` owns shared authentication and SDK transport, depending on `credential` and `forge` errors. `forge/github` depends on that client, `forge`, `record`, and Git validation mechanics; `forge/gitlab` depends on `forge`, Git validation mechanics, and the GitLab SDK. Neither adapter imports `upstream`, `publish`, nor `macports`.
- `macports/source` depends on evaluated MacPorts metadata and Tcl value decoding. It imports no forge adapter or upstream policy.
- `macports/portindex` combines index reading/querying with staging and disposable cache retention. Its dependencies are Git object mechanics, shared records, `filelock`, `fetch`, `progress`, and `tcl/syntax`; it imports neither verification nor workflow policy.
- `verify` owns provider contracts, coverage inputs, planning, judgment, and applicability. Its project dependencies are limited to `record` and Git object-ID validation. Native MacPorts evaluation and index types stay in discovery adapters.
- `macports/dependents` evaluates frozen native source and projects tool requirements, selection reasons, and index gaps into `verify.Coverage`. The planner validates that neutral input against the accepted source, platform, and roots.
- `verify/staging` consumes Git snapshots, records, PortIndex, and progress reporting to package accepted source. It imports neither a concrete provider nor workflow/state.
- `verify/tart` consumes `tart/host`, shared Tart image coordination, and PortIndex. It retains request/state lifecycle, capacity, image identity/capability evidence, guest request/result protocol, staging, and cleanup policy; mechanics do not import the provider.
- `upstream` consumes `macports/source` specifications and forge observations. `publish` consumes forge PR contracts. Neither constructs a concrete client.
- `workflow` depends on `state` and capability APIs. Capabilities do not depend back on the engine or write its records.
- `proc` supplies current-process residency around `workflow.Engine`. Requests and observations pass through state; `proc` does not judge evidence or choose the next business action.
- `outdated` owns committed-source scan lifetime, index-based selection, and per-port upstream observations. It consumes Git, MacPorts, PortIndex, port editing/probing, upstream policy, progress, and records; it imports no application, CLI, workflow, state, or concrete forge adapter.
- `app` wires concrete integrations, including SQLite, resolves application defaults and cache locations, and owns service lifetime. Multi-step capability orchestration belongs in capability packages such as `outdated`; maintenance composition and current provider selection remain application wiring/policy. Define other interfaces at actual external or test boundaries.

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

Branch-based wait/cancel and continuous integration are implemented. Authentication discovery, preflight, native login, and image-free selection of matching recorded verification are implemented. Explicit and environment credentials, Dockhand's Keychain credential, and the active `gh` account are resolved for publication; standalone and combined publication binders check identity before acceptance, and the driver repeats the check immediately before each remote effect. Device login stays outside repository state and uses Dockhand's registered OAuth client ID, with build, environment, and command-line overrides for development or alternate registrations.

Tart image setup is implemented as a state-independent application operation. It validates existing images in disposable clones and provisions a missing or explicitly rebuilt native base from a pinned guest-agent asset and an explicit MacPorts version. An explicit Xcode archive or directory creates a separate full-Xcode profile with exact version validation. Conventional release-based names allow verification to select the prepared base image when `--image` is omitted. A golden copy and candidate-first replacement order provide bounded recovery, while per-image external locks coordinate setup with concurrent verification even across different database selections.

### Implemented foundations

The state migration is complete: intake, status, cancellation, and verification cycles use `state` and `state/sqlite`, with repository registration, scoped queries, and `--db`. Cross-process tests cover concurrent writers, abandoned transactions, and competing driver claims. The [implementation report](activity/2026-09-12-sqlite-state.md) and [measurements](performance/2026-09-12-sqlite-state.md) describe the result.

Explicit branch binding and native MacPorts evaluation are now implemented. `workflow.BindVerification` uses Git snapshot mechanics and a bound `macports.Tree`; `Submit` atomically records the job and, for an existing tracked contribution, its selected revision. Standalone verification creates no contribution. Resolution initially supports a snapshot-relative port directory/Portfile or a unique directory name, plus an explicit subport. It does not use an installed PortIndex to resolve names in a different source snapshot. The [source-binding report](activity/2026-09-12-source-binding.md) describes the API, tests, and current limits.

Real Tart execution is now implemented through this path. CLI verification submission/observation and current-process residency are now connected. Revision preparation and its preview now exercise the source-transformation boundary. Durable revision-bump preparation now connects to that same engine. Explicit version bumps now use this engine too. Automatic version discovery is implemented through the same release checkpoint. Standalone publication now adds its scoped action and PR tables; review, discovery, and dependent-graph tables remain deferred. Existing domain distinctions remain available for those features.

Committed changes to a tracked branch can now become another immutable revision when explicitly bound and submitted. The [approved source-selection design](cli-design.md#approved-source-selection-and-human-edits) now specifies working-tree verification, explicit committed-branch selection, inferred contribution scope, and standalone verification without an exclusive branch association. Standalone verification and separate edited/verification targets are implemented. Working-tree capture and single-target inference from a tracked contribution are implemented. `git/changeset` owns source capture and immutable path differences; `workflow/verification_target.go` applies recorded-target selection and scope policy. MacPorts resolves and evaluates the selected target, and the driver receives explicit intent. Explicit bump versions and upstream-prefix inference are now implemented for supported GitHub and GitLab sources. Discovery, rebase, amend, and PR monitoring extend the existing packages without a second execution path.

## Groundwork status

The package skeleton, selected Tcl/source-editing helpers, SQLite state backend, workflow intake/status, and first verification cycle are present. `--db` defaults to `$HOME/.dockhand/state.db`; status reads existing state without creating a database or registering repositories. Shared worktrees and separate clones are covered by registration tests. The Git ledger, source-pin manager, and filesystem lock package have been removed.

The verification cycle handles multi-target planning, capacity waiting, admission, observation, cancellation, submission reconciliation, aggregate outcomes, and independent cleanup. It selects bounded batches through indexed queries and rechecks claims transactionally. Unsupported executors produce needs-attention outcomes. Durable version- and revision-bump preparation are implemented. Standalone and combined bump/publication are implemented. Isolated multi-attempt scheduling and PortIndex dependency queries are implemented; prepared-source integration and coverage policy, review controls, and the remaining CLI action handlers are unfinished. Automatic selection now supports the recognized GitHub and GitLab PortGroup conventions. Verification, fixed job- or contribution-selected wait/cancel, and current-process start are implemented. Tart execution now has fault-injection tests and an opt-in real build that submits in one process and settles the same run in another. See the [Tart execution report](activity/2026-09-12-tart-execution.md).

The [groundwork](activity/2026-09-10-groundwork.md), [ledger](activity/2026-09-10-ledger.md), [startup configuration](activity/2026-09-10-config-directory.md), [Cobra integration](activity/2026-09-10-cobra.md), [lockfile simplification](activity/2026-09-11-lockfile.md), [workflow intake/status](activity/2026-09-11-workflow-intake.md), and [verification cycle](activity/2026-09-11-verification-cycle.md) reports describe provenance, implementation, and validation. The [lock-directory report](activity/2026-09-12-lock-directory.md) records the former configuration; the [state design report](activity/2026-09-12-state-design.md) and [implementation report](activity/2026-09-12-sqlite-state.md) document its replacement. Earlier reports remain historical records. The [behavioral test report](activity/2026-09-11-behavior-tests.md) describes the first permanent tests for `workflow`, `ledger`, and `tcl/syntax`. All tests were authored for v2; none were copied from v1.

The [performance pass](activity/2026-09-12-performance-pass.md) batches Git source validation and avoids transactions for known ineligible cycle work. Its measurements motivate replacing whole-ledger writes with affected-row updates; the reports and benchmark inputs remain useful comparison evidence.

### CLI execution and attachment

`app.Services.BindVerification` composes native platform discovery, Tart configuration capture, and workflow source binding. Cobra parses input, submits through the engine, renders recorded progress, and selects an attachment milestone. `workflow.Reached` evaluates admission/completion from records. `proc.Manager` owns the cancellable loop shared by targeted attachment and resident execution; it owns no workflow state and creates no driver discovery records. A cycle that records job advancement runs again immediately so ready phase transitions do not inherit the idle polling interval. A pass without job progress waits before polling again. Resident passes use indexed cycle queries and do not build full historical status snapshots.

The Tart guest runs lint, build, declared tests when enabled, and installation in that order. Build, test, and install enable debug output, which goes to the same retained log streamed by `--trace`; lint remains quiet. Build failures stop the sequence and record a separate build step. Dependency binaries remain enabled unless `--from-source` is selected.

`macports/portindex` prepares a PortIndex before the provider stages a frozen source and serves the same generations to discovery and dependent selection. `--prefix` selects the host MacPorts `portindex`; its executable digest participates in frozen provider settings, and `ResolveTool` also probes the MacPorts Base the launcher loads. One cache root holds an environment per indexer digest, Base runtime, and platform; each environment stores immutable, atomically published generations keyed by source tree with metadata recording the seed, strictness, and pass type. A request reuses an exact generation, otherwise derives from a seed: the recorded base for a candidate, else the environment's latest pointer or a recently used generation whose Git tree diff avoids `_resources`. Only the base-like trees advance the pointer. Strict requests never reuse a generation built without `-e`. Builders serialize per generation while readers hold the environment lock shared; collection takes it exclusively. The mirror URL remains recorded provenance but is not used, because a mirrored index cannot prove which source tree it describes; see the [storage design](portindex.md).

`verify.LogReader` is an optional read-only diagnostic interface. Tart reads bounded guest log ranges while running and retained host logs after collection. CLI tracing keeps offsets, drains final logs, and writes to stderr without participating in workflow bookkeeping. The [CLI execution report](activity/2026-09-12-cli-execution.md) records scope and validation.

### Preparation previews and version-selection groundwork

`workflow/preparation.Service` owns source materialization and final Git tree storage. `macports/portedit.Service` owns target evaluation, edits, and fidelity checks. Probes reuse a disposable materialization and restore its Portfile after each evaluation, keeping the frozen PortGroups and local files available. The final tree is independently materialized and checked against the editor's result. `git.EditTree` retains file preconditions and never changes the user's index or refs.

`app.PreviewPreparation` constructs only the capabilities a preview needs. `bump-revision --diff` renders the real result without state or a provider. Literal subport selection and sibling fidelity work through complete MacPorts evaluation. The driver uses the same capability for job progression, guarded branch integration, and verification continuation. Working-tree input is supported by verification; bump previews continue selecting committed source and do not adopt a branch association.

`upstream.MatchRelease` separates deterministic explicit-version/tag selection from evidence collection. `upstream.Resolve` now collects exact tag observations through a bound `forge.Repository`; GitHub and GitLab supply concrete implementations. `upstream.Check` validates the recorded forge, instance, repository, tag, and commit during preparation. `upstream.DiscoverPort` selects an eligible stable numeric version from the source's tag or release catalog. The [groundwork report](activity/2026-09-13-bump-groundwork.md) and [explicit-version report](activity/2026-09-13-explicit-version-bumps.md) record the earlier slices and their provenance.

GitHub-backed `go.setup` now shares the version editor and preparation pipeline with `github.setup`. `macports/portedit` owns the version argument edit and checks the evaluated `go.version` change. `macports` inspects native fetch targets and reports `fetch.archive_compatible`: standard archive fetching and the structurally recognized Go toolchain pre-check are supported. Raw hook bodies stay within the adapter’s RPC/decoding boundary. Unknown hooks or changed hook structure require another preparer; no arbitrary hooks run during preview. MacPorts executes the normal hooks in the verification VM. See the [Go-port preparation report](activity/2026-09-13-go-port-preparation.md).

### Durable preparation and integration

`workflow/preparation_source.go` binds source and accepted choices. `preparation.go` claims immutable preparation and checkpoints a candidate commit. `integration.go` reconciles the destination ref and records the contribution/result revision. The consumer-owned `SourcePreparer` interface exposes the existing preparation capability; it does not add another workflow abstraction. `verification.go` uses the same attempt lifecycle for standalone inputs and prepared results.

`git/branch.go` provides Git identity lookup and branch-specific exclusion. Its descriptor reaches short Git subprocesses through the existing repository command boundary. This lock protects ref effects and recovery, while SQLite continues to authorize state transitions. Schema 3 adds job claim/checkpoint fields and permits standalone plans/attempts without contribution revisions; it introduces no table or package. The existing `x/sys` dependency is now direct.

`app.Services.BindPreparation` captures source, platform, author, and verification configuration. Shared CLI build flags feed `verify`, `bump`, and `bump-revision`; attachment and progress rendering remain shared. The [revision-driver report](activity/2026-09-13-revision-driver.md) records behavior, recovery limits, and validation.


`macports/portedit/source.go` loads the disposable workspace and evaluates candidate contents. `macports/portfile` supplies literal candidates; the editor validates actual upstream values through MacPorts. A full source literal is tested directly, so an invalid artificial probe cannot rule out a valid release. For composed source references, shape-preserving probes establish a possible literal substitution. Actual edits must select the requested source reference and produce an unambiguous evaluated version. Unrelated metadata and sibling changes are rejected for the selected update; final checksum edits are compared with the observed version-only baseline.

`upstream` retains catalog access, livecheck eligibility, and native MacPorts version comparison. The editor supplies a version-evaluation callback for candidate observations, and a batch form that `Bind` adopts when the probe offers it, so a catalog with hundreds of releases shares one interpreter instead of starting one per candidate; discovery then validates the selected edit before returning a release. No calendar transform or arbitrary Tcl inverse is assumed. `record.Release.Version` holds the evaluated MacPorts version; `Tag` and `Commit` identify the upstream source. The workflow's `ReleaseResolver` checkpoint remains separate from preparation so interrupted runs retain the selected source.

Schema 4 adds one immutable-once-set JSON column to `jobs` for the selected release. It introduces no table, state interface, lock, or generic coordination abstraction. `golang.org/x/crypto` provides the legacy RIPEMD-160 checksum MacPorts Portfiles use. The existing HTTP client injection points support independent network tests. GitHub discovery can use public access; publication resolves an explicit token, environment token, Dockhand Keychain entry, or `gh` credential. Credentials are not persisted in the release checkpoint or workflow database.

## Automatic version selection

Automatic and explicit bumps share `upstream.Resolve`, the release checkpoint, preparation, integration, and verification. `upstream/latest.go` owns eligibility and current/update/unknown assessment. A bound `forge.Repository` lists tags, and repositories that also implement `forge.ReleaseRepository` can supply releases. `forge/github` uses `go-github`; `forge/gitlab` uses GitLab's Go SDK. Both SDKs own their API requests and pagination defaults. GitHub and GitLab PortGroup conventions are isolated in `macports/source`; the forge adapters contain no Portfile policy. `macports/versions.go` and its Tcl script implement `VersionSelector`, applying the port's evaluated list-encoded regex to generated candidate match text and using native `vercmp` ordering. The evaluator preserves raw Tcl option values; list consumers interpret them at their own boundary.

An automatic `record.Release` additionally retains `CurrentVersion` and `NoUpdate`. The workflow atomically records a current observation and completes the job without invoking preparation or verification. Existing JSON storage accommodates these optional fields without a schema migration. See the original [automatic-selection report](activity/2026-09-13-automatic-version-selection.md) and the [GitHub/GitLab source report](activity/2026-09-14-github-gitlab-sources.md) for scope and validation.

## Working-tree verification

`git/worktree.go` captures raw bytes for paths tracked by the selected worktree's index, including staged additions and deletions. It preserves index flags in the real index while independently reading working files, computes Git blob identities, writes changed blobs, and assembles a tree with a disposable index. SHA-1 and SHA-256 repositories are supported. Two content passes plus index/HEAD checks detect ordinary concurrent changes; sparse entries and conflicts are refused.

`workflow.BindVerification` chooses current-checkout capture when `Branch` is empty and committed binding otherwise. Both inputs share isolated materialization, MacPorts target resolution/evaluation, acceptance, and the existing verification cycle. A dirty source has a tree without a commit; `record.Checkout` separately records branch, observed HEAD, and modified-file count in immutable job options. Tracked contributions retain revision association; standalone and detached verification create no contribution. SQLite's existing options JSON stores this provenance, with no schema migration.

`verify.PlanSingle` and Tart accept tree-only sources. Tart still checks commit/tree agreement whenever a commit is supplied, and always materializes the accepted tree. Guest input, durable provider identity, recovery, and cancellation remain on the existing path. Evidence reuse after later commits and tracked-target inference are implemented; standalone publication can use matching evidence. See the [working-tree report](activity/2026-09-13-working-tree-verification.md).


## Verification reuse

`verify/reuse.go` compares complete build inputs, checks partial accepted build requirements, and judges recorded evidence without querying storage or calling a provider. `workflow/reuse.go` selects from bounded original-attempt candidates and records the decision during initial planning. A prepared job without an image may select an exact prior configuration only when its provider, platform, test, and source-build requirements match. A newer matching negative result prevents reuse of an older pass. `state.Reader.VerificationCandidates` supplies repository-scoped history through indexed SQLite queries; schema 5 adds the job's original-attempt reference and diagnostic detail. The requirements fit in the existing job options JSON and add no migration, cache package, or duplicated evidence record.

Tart records a verifier digest alongside the existing image digest and frozen settings. The digest includes guest code, the guest-command descriptor-isolation wrapper, its launch description, and an explicit host-protocol version marker. Changes to host execution semantics that are not represented in those inputs must advance that marker. Missing legacy identities disable reuse. The CLI exposes `verify --fresh` and projects the original attempt separately from a job's own executions. See the [reuse report](activity/2026-09-13-verification-reuse.md).


## Forge and upstream boundaries

`upstream.Catalog` accepts the interpreted forge instance and repository name and returns a bound `forge.Repository` without performing an observation. Both concrete clients satisfy that small consumer-owned interface directly. The repository always supplies exact-tag and tag-catalog access; GitHub additionally supplies the optional release catalog. This prevents tag and release readers for one selection from addressing different repositories.

`forge/github` uses the pure-Go `go-github` library for typed API operations, request construction, authentication, API headers, JSON, and pagination metadata. SDK types stay within `github`, `forge/github`, and `verify/github`; other capabilities use domain contracts. The shared `github.Client` owns credential discovery, authenticated-user checks, same-origin read redirects, disabled write redirects, and lazy SDK initialization. The forge adapter retains observation validation and publication-outcome classification. Both adapters share the client without persisting credentials. The source interpreter admits only the public GitHub PortGroup instance; GitHub's configurable API origin remains a transport and test setting.

`forge/gitlab` uses the official GitLab Go SDK for exact tags and complete tag catalogs, including self-hosted instances. A path in `gitlab.instance` is the PortGroup's leading project namespace, so the adapter adds it to the encoded API project identifier while using the URL origin as the API base. This matches existing MacPorts conventions such as `https://gitlab.torproject.org/tpo`. GitLab releases and publication remain outside this slice.

Repository clone URLs and PR/release web URLs come from SDK response fields where those fields exist. Clone locations are checked against the intended repository before Git uses them; PR identity comes from its repository and number. Candidate match text is different: `macports/source` renders the archive-shaped GitHub text or Atom-entry-shaped GitLab text that the evaluated PortGroup regex expects. It is never used to download source. Source downloads continue to follow evaluated `master_sites` and `distfiles`. The earlier [URL and resource audit](activity/2026-09-13-github-resource-urls.md) describes the GitHub-only boundary before this split.

`macports/source` interprets evaluated GitHub and GitLab PortGroup fields and recognizes their supported livecheck conventions. Its `TagPattern` supplies one prefix/suffix mapping for explicit selection, automatic filtering, and source checks. `record.Release` now retains forge and normalized instance alongside repository, version, tag, commit, and observation time, so a shared database can distinguish identical repository paths on different GitLab hosts. Tcl option collection remains in `macports`; source edits to `github.setup`, `gitlab.setup`, and GitHub-backed `go.setup` plus fidelity checks remain in `macports/portedit`. Those operations concern Portfiles, not forge APIs.

Missing-ref classification happens only in exact-tag lookup. The SDKs preserve their structured API errors, so a catalog failure cannot become evidence that a requested tag is absent. Each SDK owns API defaults and pagination. The adapters add no record/page counts, body-size bounds, page-size overrides, or operation timeouts. Discovery and tag lookup use the caller's context; workflow call deadlines and cancellation remain in effect. See the earlier GitHub [refactor report](activity/2026-09-13-forge-upstream-boundaries.md) and the [GitHub/GitLab source report](activity/2026-09-14-github-gitlab-sources.md).

## Implemented publication boundary

`workflow.BindPublication` selects a committed source and its applicable verification. For an untracked branch, `publish/source.go` derives its single parent and changed port directory; a tree/Portfile lookup supplies the latest verified target and configuration. Acceptance creates the contribution and revision with the publication job in one transaction. `publish.Plan` owns contribution scope, destination selection, content, and remote preconditions; `publish` also owns PR observation validation and metadata policy. `forge/github` owns GitHub protocol and URL rules. `git` owns remote inspection, literal-ref pushes with explicit expected values, ancestry checks, and the local operation lock. `state` exposes only publication and PR methods required by this path; SQLite migration 6 supplies their storage.

The shared cycle claims the job, takes a lock for its forge/head-repository/head-branch, rechecks ownership and cancellation, and records a checkpoint before each external effect. No state transaction spans Git or HTTP. PR uncertainty selects observation-only recovery. A confirmed rate-limit refusal advances the durable refusal counter and permits retry after cooldown; an ambiguous response never authorizes repeating a create or update. Status joins the action and retained PR association. See the [publication report](activity/2026-09-13-publication.md) for boundaries and tests.


## Verification performance and timing

`verify/tart/image.go` owns prepared-image content identity and metadata checks. `state.ImageCache`, included in `ProviderStore`, keeps the storage boundary independent of SQLite. The backend adds a provider/path cache in schema 7; it does not introduce a new package, generic lock service, or alternate source of verification evidence.

`workflow/timing.go` owns operation deadlines. Each preparation, provider, publication, or cleanup action uses the corresponding budget for both its call and its expiring claim. `ObserveInterval` determines the persisted next observation for a running build, while `WaitInterval` governs expected capacity/run/publication waiting. `workflow/retry.go` uses `RetryDelay` as the initial failure delay, then applies capped exponential backoff, jitter, and forge retry deadlines. Counters and deadlines are durable; cancellation can bypass pending verification waiting. Other drivers respect the recorded deadline. CLI status/log refresh has its own cadence and reads recorded progress without forcing a provider observation.

CLI completion reports the verification outcome directly. Reuse explanations remain separate progress/status details, and detach output names the driver's remaining settlement and cleanup responsibility. See the [activity report](activity/2026-09-13-verification-performance.md) and [measurements](performance/2026-09-13-verification-overhead.md).

## Combined preparation and publication

`publish.Destination` resolves immutable destination choices at intake; `publish.PlanTo` combines that destination with a verified prepared revision and current remote preconditions. Standalone `publish.Plan` composes both. `workflow/publication_plan.go` owns the durable transition between verification and publication; the existing publication executor owns push, PR writes, confirmation, and recovery for both command paths. No new package, dependency, schema, or child job is introduced.

`record.JobSpec.PublishTo` holds a combined job's destination. `record.PublicationSpec` remains the complete external-action intent, populated only once the prepared revision passes verification. Workflow checks the action against the accepted destination and prepared revision before persisting it and before claiming it for execution; SQLite preserves the resulting intent and its relationships. Verification and publication read the result source without replacing the accepted source. See the [combined publication report](activity/2026-09-13-combined-publication.md).

`macports/dependents` stages a source-matched PortIndex, selects direct build/library/runtime dependents, and evaluates independent target configurations. It projects native results into `verify.Coverage`, including source-bound Xcode requirements; raw evaluator snapshots and index query structures stay inside the adapter. It preserves selection reasons, evaluation failures, and indexed closure gaps. Its closures are default-variant estimates, not guest dependency resolutions. This discovery boundary performs no state writes, revision edits, or provider operations; workflow records the resulting plan and applies accepted per-target configurations.

### Transient operation progress

`progress` carries optional scoped messages through the active call context. The CLI installs a serialized observer that prints escaped stage messages on stderr, including with `--json`. Tart and PortIndex report actual work boundaries without importing the CLI or persisting display text. This does not change durable job states, provider capacity, or admission semantics. A different driver reports its own activity; status readers still use recorded state.

`tui` renders the live `status` table with Bubble Tea. It depends only on `workflow` for the contribution projection and on three closures the CLI supplies: one that polls the snapshot, one that runs a dockhand verb in-process with its output captured, and one that opens a URL or file. It holds no state of its own beyond the selection and the message strip, so a key can do exactly what the corresponding command does and nothing else.

### GitHub verification

`internal/verify/github` owns committed-source eligibility, the supported MacPorts workflow shape, durable submission and recovery, matrix outcome interpretation, per-request tracking cancellation, and completed-job log caching. Canceling tracking does not cancel a shared Actions run. `internal/verify/github` binds the shared authenticated SDK to Actions operations through its private Actions interface. `internal/app` resolves the personal fork through publication's destination logic before acceptance.

`internal/workflow` selects providers by the names recorded on attempts and resource handles, independent of CLI defaults. Both publication and GitHub verification use `internal/git`'s remote-branch lock and conditional push. `record.WorkflowEvidence` records remote run attempts and matrix jobs separately from locally observed environment and port-phase evidence. SQLite persists the selected branch in job/attempt JSON options; no schema migration is required.

### Shared installation inspection

`macports/installation` owns MacPorts installer selection and installation facts through an explicit command target. It reports version, platform, active ports, and probe diagnostics. `macos` supplies OS/toolchain and foreign-package-manager observations. Provisioning applies its requested image profile and explicitly runs compiler/package checks; verification applies accepted build requirements. These consumers do not duplicate installation probing or move their policy into the observer. `tart.Client.Images` owns the CLI image-list representation, while callers own filtering and lifecycle decisions.

### GitHub client and adapter boundary

`github.Client` owns configuration, credential selection, OAuth device login, lazy SDK construction, redirect policy, and rate-limit translation. `app` shares one instance with `forge/github` and `verify/github`. Repository/tag/release/PR mapping stays in the forge adapter. Actions requests, workflow configuration, matrix interpretation, and run recovery stay in the verification adapter. Its SDK-typed Actions interface is private; configuration returns a recorded build configuration, so application wiring does not inspect SDK objects. Neither adapter imports the other, and `workflow` imports neither adapter nor the SDK.

### Editing and transfer boundaries

`macports/portfile` owns syntax-aware revision/checksum replacement, accepting evaluated values and checksum data without knowing downloads or releases. `portedit` owns evaluation and fidelity around those operations. `fetch.Open` supplies bounded HTTP response bodies to source-archive and PortIndex consumers. Callers supply their own byte limits and deadlines and retain content checks, hashes, file commit semantics, and cache ownership. GitHub log retrieval continues through its SDK adapter rather than adopting archive download policy.

### Verification execution boundaries

`workflow` keeps planning/reuse, due-attempt selection, and result recording in focused files within the same package. `advanceJob` retains the transaction/claim/provider-call sequence; these helpers introduce neither a second scheduler nor a new state owner. Provider calls remain outside write transactions, and recording still checks the claim before adopting results.

### Human corrections

The [human-correction design](human-corrections.md) defines ordinary Git editing, managed amendment/rebase, explicit branch reassociation, and conditional updates to an existing PR. These extend the same capture, revision, verification, and publication boundaries. Managed commands now use the existing preparation lifecycle, with private Git candidates, guarded ref adoption, and immutable replacement revisions. `git/changeset` composes correction commits; workflow retains change identity and SQLite authority. Publication records separate local and PR head branches.

### Dependent verification

`macports/dependents` discovers the source-bound cohort and index gaps. `verify.PlanDependents` converts those observations into immutable target questions and per-target requirements, including roots to build/install in downstream guests. `workflow` claims discovery outside the writer, records the complete plan once, and uses the existing attempt scheduler and capacity handling. SQLite stores the additional intent and build inputs in existing JSON fields; there is no schema migration or graph scheduler.

Tart supplies the recorded index recipe and executes preinstalled roots in each isolated guest. `verify/staging` checks that roots and the main target are indexed. The application adapter only composes that frozen recipe with source discovery. Publication follows the root attempt's immutable plan and requires applicable passing results for the entire cohort; `publish` formats the resulting evidence. The default image covers the cohort; explicit per-dependent images are resolved by `app` through Tart at intake, stored in job options, and applied by `verify.PlanDependents`. Every choice stays on the root platform and retains required tools and build policies. Recovery uses the recorded configurations.


### Read-only upstream selection

`app.Outdated` constructs `outdated.Service` with the selected repository, native evaluator, upstream adapters, and cache configuration. `macports/survey` captures committed local HEAD and selects ports in an owned disposable workspace; `outdated` observes those ports through source adapters and the version probe. `assess` shares that source and selection boundary. `macports/portindex.Filter` owns exact maintainer/category metadata matching and reports unknown coverage for omitted Portfiles, missing subports, and unread fields. The capability stages a source-bound index in the caller-selected system user cache without opening state; the CLI owns selector flags and rendering. Discovery does not accept jobs or grant publication authority. Primary-port-only version probing remains an explicit unknown for selected subports.


### MacPorts runtime diagnostics

`macports/compatibility.go` describes the observed Base/Tcl/platform and historical source-review coverage. Its Tcl companion checks startup interfaces, worker option access, and native fetch-target registration before interpreting actual fetch hooks. `Evaluator.Inspect` supplies setup diagnostics; snapshots retain the runtime observation. `macports/portedit` preserves specific fetch-compatibility failures when refusing preparation. There is no version-selected adapter or new package; [compatibility evidence](macports-compatibility.md) distinguishes runtime tests from source review.

### Tart runtime and VM control

`Client.Resolve` canonicalizes existing directory ancestors without initialization, so missing final paths and symlink aliases retain a stable Tart-home identity. Configuration inspection does not create the verification artifact root; a request operation initializes it.

`host.Machine` supplies guarded clone/delete, launchd-backed VM lifetime for verification, foreground process startup for provisioning, and guest execution with argument/stdin preservation and inherited guest-descriptor cleanup. Provisioning owns its foreground process bookkeeping and recipe-specific readiness/stop deadlines. Verification owns the build runner's paths, launch conditions, results, and evidence. Its accepted guest protocol digest is unchanged by the extraction.

`macos.LaunchdPlist` renders launchd XML for both host VM services and guest build services. `atomicfile.Write` preserves the existing synced temporary-file/rename/directory-sync sequence for VM plists and retained verification results. These are concrete shared mechanics, not a general executor or VM framework. See the [VM-control activity report](activity/2026-09-15-tart-host.md).

### Disposable cache retention

`workflow.Collect` selects terminal jobs/attempts for the optional `verify.LogCachePruner` capability. The GitHub provider owns cache naming, local age checks, and request-lock serialization; no workflow evidence or remote data is deleted. `app.Collect` composes this with `macports/portindex.Collect` for the shared discovery and selected Tart index roots. PortIndex owns profile-lock coordination and last-use tracking; its subprocess inherits that lock during index construction. `filelock.TryExisting` provides nonblocking acquisition without initialization so previews and collection skip active users. Cache cleanup introduces no schema or generic storage collector. See [state operations](operations.md#index-and-github-log-cache-retention).

### Preparation assessment

`assess.Service` coordinates source selection and optional explicit-release resolution. `app.Assess` only wires integrations; `cli` owns flags, formatting, and exit status. Assessment does not depend on workflow or state. `macports/survey` shares committed-source materialization, selection, index staging, coverage problems, and cleanup between assessment and outdated discovery.

`macports/portedit.VersionProbe.Assess` owns preparation findings and input locations. Its release-specific checks reuse the archive plan used by actual preparation, including fetch/checksum associations and fidelity. Dependency declaration inspection and primary-source stripping are also shared with preparation. Helper availability is checked without executing the generator; downloaded manifest equivalence remains untested. A typed inconclusive-probe error preserves uncertainty without interpreting error messages.

No new preparation engine, forge client, or workflow path is introduced. PortIndex reading remains alongside staging/cache management: adding an explicit all-port filter did not require a second consumer abstraction within that package.

The bump preparation seam also includes `macports/distfiles`: it binds native fetch plans to evaluated checksum declarations and exact source spans. `portedit` owns context coverage, intended changes, protected pins, candidate selection, downloads, and final fidelity. `eval` owns request-scoped modeled sessions and reports the host runtime separately. `upstream` distinguishes resolved forge tags from explicit and livecheck-discovered archive versions; neither state nor workflow interprets Tcl.

HTTP release listings are interpreted in `macports/source`, collected through `fetch` by `upstream`, and parsed/compared using Tcl regex and MacPorts version semantics in `macports/eval`. The evaluator also supplies structured standard/guarded/custom fetch facts. `portedit` retains rejected-platform restrictions and completes local candidate planning before dependency source transfers or generator execution.

### Routine resource retention

Accepted job options own the explicit `KeepFailed` policy, separate from build/evidence identity. `workflow` settles terminal attempts through its existing claimed resource-release path and selects bounded batches of old released diagnostics. `state/sqlite` supplies indexed queries and immutable released ownership with pruning retry/checkpoint fields. `verify/tart` owns removal of acknowledged transfer copies and released diagnostic directories under its submission lock. No transaction spans VM or filesystem operations; reusable caches retain their separate explicit collection path. See the [cleanup report](activity/2026-09-16-routine-cleanup.md).
