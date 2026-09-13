# Architecture

This document applies the [design principles](principles.md). The [CLI design](cli-design.md) specifies command names, attachment behavior, and user-visible results. The [component structure](components.md) maps these responsibilities to Go packages. The [state-store design](state.md) specifies persistence: SQLite replaces the Git ledger. The initial migration is implemented; current scope and validation are recorded in the component document.

## Configuration

Dockhand uses the user's global Git configuration for workflow defaults. Repository Git configuration can override those defaults, and explicit command options take precedence for the submitted request. Database selection is independent: `--db` selects a file, with the same home-directory default across checkouts.

Accepted jobs record the effective choices needed to explain and reproduce their work. A later configuration change must not silently change an accepted job's source, build question, or requested destination. Credentials remain outside durable job records.

### Database selection and initialization

The global `--db PATH` option selects the state database, defaulting to `$HOME/.dockhand/state.db`. It replaces `--lock-dir` and its `-L` alias, with no compatibility alias. There is no config-directory setting, and `DOCKHAND_CONFIG_DIR` is not consulted.

Cobra resolves the home-directory default and passes the selected path through `app.Config.DBPath`. Relative paths resolve against the invocation's working directory; an explicitly empty path is an error. The flag accepts a file path, with file completion, rather than SQLite URI parameters. Help displays the resolved default without opening state.

Open state only when the command needs it. Writable opening creates a missing parent directory and database with restrictive permissions, initializes the schema, and registers the selected repository. Read-only status does none of those things: an absent database or unregistered repository produces empty status; unreadable, corrupt, or unsupported state is an error. Help, completion generation, and previews do not open the database. `app` owns connection lifetime and supplies the state contract to workflow services.

### Repository scope

One database holds work for multiple repositories. `app` discovers and canonicalizes the selected checkout's Git common directory and resolves it to a database repository ID. Linked worktrees share an entry; separate clones remain distinct even with the same remote. A moved repository requires explicit reassociation rather than automatic matching by URL.

Every workflow read and transaction has an explicit repository scope. `status`, selectors, targeted cycles, and `start` initially operate on the selected repository; an all-jobs selection does not mean the whole database. Repository-qualified relationships prevent records from different repositories being joined accidentally. The driver retains each accepted job's repository context.

## State and transactions

SQLite holds authoritative workflow metadata behind `internal/state` contracts, implemented by `internal/state/sqlite`. Git remains the source repository, with no authoritative state ref, derived ledger, source pins, or Git operation journal. Logs, downloaded sources, artifacts, and VM images may live outside the database; records preserve their identities, evidence, and retention obligations.

The store exposes consistent read-only views and short write transactions through record-specific methods and bounded queries. It never exposes a whole-database map for mutation. A write reads and updates the affected rows and indexes. Request acceptance, claims, and related lifecycle changes commit together. `workflow` chooses transitions; the backend provides atomicity, isolation, constraints, and physical locking.

Transaction callbacks run once, synchronously, and only validate or change database records. They use the supplied context, cannot escape their transaction, and must not call provider, forge, Git, or other external operations. An expired callback cannot proceed to commit. Deadlines are cooperative, so they cannot forcibly terminate arbitrary callback code. Nested store calls and automatic callback replay are unsupported.

The SQLite implementation uses WAL, foreign keys, short snapshot reads, and immediate write transactions. SQLite serializes writers across the database, including different repositories; the design reduces contention by keeping each write small. Busy waits and operation deadlines are bounded. No configurable lock directory or separate filesystem writer lock remains. See [state.md](state.md) for schema, opening modes, transaction contracts, and validation.

Only confirmed commit returns an acceptance receipt. An interrupted or uncertain submission is retried with the same durable request ID and immutable intent. Read time remains separate from provider or forge observation time. Reading status neither claims work nor refreshes external observations.

### Git availability and interrupted work

Persisting object IDs does not retain their Git objects. Before an operation consumes source, it checks the specific objects it needs. If they are missing, preserve the recorded identity and evidence and report the affected work as needing attention. An unreadable repository is distinct from confirmed missing objects. Existing provider runs and cleanup can continue when they do not need that local source; never retarget accepted work to the current branch head.

Preparation uses isolated job-owned workspaces and recorded inputs. After interruption, inspect the workspace and expected branch state before resuming or retrying; ambiguous integration requires attention. SQLite and Git do not form one atomic transaction. Add a candidate revision checkpoint inside ordinary job/revision records only when preparation needs it, without a generic Git-operation table. Optional identity notes and branch-reassociation commands remain later work.

## Changes, revisions, and jobs

The `record` package defines these shared records, their identities, and the value types they contain. Package and type documentation lives alongside the definitions in `internal/record`; workflow transitions and state persistence remain in their respective packages.

A tracked change represents the logical contribution. Give it a stable identity independent of a branch name, commit SHA, job, or PR number. Initially it belongs to one local repository and one branch association. That branch can advance while the change ID stays fixed. A missing or renamed branch produces an actionable error; explicit reassociation can be added later. The change relates affected ports, revisions, jobs, and any associated PR, with a lifetime beyond an individual job.

A revision identifies an immutable source snapshot and its upstream base. Preparation records how a new revision relates to its predecessor. Rebasing or incorporating corrective edits produces a new revision of the same tracked change. Rewriting commits must preserve the change identity and PR association, along with the earlier revisions and evidence needed for history and recovery.

A job is one accepted request: prepare a change, verify an immutable snapshot, or publish a selected revision. Contribution work associates the job with its change; standalone verification does not require creating a tracked contribution. Record its input revision and any resulting revision explicitly. Each follow-up request has its own outcome; it does not reopen a completed job or silently retarget an active build to a moving branch. An initial preparation job may create the change and its first revision.

Publication has two separate concerns: an action that opens or updates a PR, and the continuing association with that PR. Keep the PR identity, last confirmed published revision, current local revision, and latest observed remote head distinct. A publication job finishes when its requested revision and metadata have been confirmed on the forge. It does not wait for merge. The tracked contribution can remain open through further review, revisions, and jobs, until its disposition is recorded as merged, closed, or abandoned.

A later branch edit does not inherit authorization to publish from a completed job. Likewise, an older verification finishing after a new revision is prepared records evidence for its own inputs; it cannot silently publish the older revision over the new one. Serialize mutations to a change and check expected local and remote heads before applying them. Independent builds can continue against their recorded inputs while those inputs remain available.

These are domain distinctions, not a requirement for separate services, packages, or a generic workflow engine. Plain records with explicit identities and relationships are sufficient. Establish them in phase one even where the user-facing actions arrive in phase two.

### Approved source-selection extension

The [CLI source-selection design](cli-design.md#approved-source-selection-and-human-edits) extends the initial committed-branch implementation. Users address contributions through branches and may edit or rebase with ordinary Git commands. Source binding captures their selected input without requiring a separate adoption command on each edit. Stable change identity remains in state; a branch is its user-facing association, and each job still names exact accepted work.

Current-checkout verification will freeze tracked working-tree contents, including staged additions, without modifying the user's index or branch. Explicit branch selection will freeze committed contents. Git supplies snapshot mechanics, MacPorts evaluates that complete snapshot, and workflow accepts the immutable input with its selection provenance. No provider reads a mutable checkout during a queued or running build. Detect conflicting source changes during capture rather than knowingly accepting an inconsistent snapshot; source availability and recovery follow the existing rules above.

A contribution's edited ports and its intended verification targets are separate sets. Reconcile externally edited source with the recorded intent, and surface ambiguous scope before scheduling work. Standalone verification must retain its source and targets without claiming the branch as an exclusive contribution. The current one-branch/one-target binding limitation must be removed as this behavior is implemented; it is not the intended contribution model.

Evidence remains attached to the original attempt, but applicability can extend to a later revision with the same complete tree and compatible build inputs. Commit identity alone does not define build equivalence. Include target/configuration coverage, platform/environment identity, artifact inputs, and any other build-relevant context in that decision. Publication requires committed source and checks applicability to that selected source; it does not inherit a pass from matching only the port's version or some edited files. Identical committed contents can reuse evidence from a working-tree snapshot without rewriting its history.

### Explicit bump versions

`bump <target> [version]` preserves the user's optional requested version in accepted intent. Omission requests automatic latest-release discovery. Resolution keeps that input distinct from the MacPorts version and upstream tag/reference. When a prefix is omitted, the current Portfile's evaluated upstream reference and version-to-tag convention supply a candidate, which must be confirmed against upstream evidence. Explicitly supplied prefixes are preserved; ambiguous or unavailable resolution remains visible rather than selecting an unrelated release.

`upstream` owns release/reference resolution and returns evidence; `macports` supplies bound source metadata; `prepare` uses the confirmed selection to produce edits and re-evaluate the resulting port. The CLI parses and displays the selection. Resolution performs no external calls inside a state transaction. Workflow records the concrete resolved source with preparation progress so recovery uses the same release instead of rerunning latest-version discovery against a changed upstream. These distinctions fit the existing packages; no new dependency or general version framework is required by this design.

## The Driver

The driver advances accepted jobs toward their requested destination: preparing a branch, verifying it, or publishing it as a pull request. Publication requires an explicit request and the applicable policy decision.

A cycle is one reconciliation pass. It makes bounded progress on eligible work:

- Observe submitted builds and outstanding forge actions, then record evidence and outcomes.
- Assign ready verification work to compatible providers with available capacity.
- Advance publication requests whose evidence and policy requirements are satisfied.
- Recover abandoned claims and reconcile actions whose external outcome is uncertain.
- Fulfil cancellation, release, and retention obligations while preserving the evidence needed for diagnosis.

Failure in one job or an error polling one provider must not abort unrelated work for the entire pass. A pending dependency blocks only the work that actually requires it. Retries are bounded and classified; a known build failure is not an invitation to rebuild indefinitely.

### Driver execution

`dockhand` itself is the driver. There is no separate driver executable, executable-path configuration or lookup, or automatic background launch. The `proc` component manages residency and loop lifetime within the current process.

Commands such as `bump`, `verify`, and `publish` submit requests through the shared workflow API and run targeted driver cycles within that invocation. One invocation may run multiple cycles: waiting for capacity, observing a build, and completing publication require later passes. With `--wait`, it continues through the requested destination or a conclusive outcome.

The user explicitly starts persistent mode with `dockhand start`. It repeatedly processes eligible repository work through the same workflow implementation. Ongoing PR monitoring is a phase-two extension of this mode. State transactions and claims coordinate concurrent action invocations and persistent execution; starting a driver grants no additional publication authority.

Once an action invocation exits, further workflow advancement requires an already-running persistent driver or another invocation that runs driver cycles. Submitted provider builds may continue independently, while settlement, publication, and cleanup remain durable obligations for later cycles.

**Driver ownership and job milestones**

A job is the driver's durable record of an accepted operation. It names the target branch or revision, the requested work and destination, and the verification or publication work needed to get there. It may require several provider builds, including downstream dependent builds. It is not synonymous with one VM or one provider request.

| Milestone | Meaning |
| --- | --- |
| Accepted | The shared workflow submission API has durably recorded the queued job in the database. The CLI receives its job ID; driver pickup may happen afterward. |
| Admitted | The verification provider has accepted the initial build request. Persisting a local queue entry does not count as provider admission. |
| Completed | The requested destination has been reached and its outcome recorded. Publication completion means the PR was opened or updated, not that it was merged. |

A refusal, failed verification, or condition requiring attention is reported explicitly when the requested destination cannot be reached. It must not be displayed as successful admission or completion. A job can also complete from matching existing evidence without starting another build.

After durable submission, the driver owns status recording, capacity waiting, provider submission, evidence collection, retries, publication, environment retention/release, and recovery bookkeeping. Submission and driver pickup are separate: a recorded request is recoverable even if no driver has claimed it yet. The CLI must not independently poll the provider to decide a verdict, settle a build, or release an environment. It displays the driver's recorded state and progress.

## Request handoff

The state store carries requests from command invocations to driver cycles and carries recorded progress back to observers. There is no Unix socket or separate local request transport. The CLI invokes shared `workflow` intake functions to persist requests transactionally, rather than constructing or mutating workflow records itself. Resident and targeted drivers read and claim eligible work through the same repository-scoped store.

The accepted milestone means the durable intake transaction succeeded. Driver pickup is a later claim, and provider admission remains a separate milestone. Starting or finding a process proves neither. An interrupted submission can be looked up by its request ID. Exiting the invoking process leaves the recorded request available for recovery by a later cycle.

`wait` and the waiting portion of other commands observe database records and may run targeted cycles through the shared workflow engine in the same process. Here, attachment means remaining present to observe progress, not maintaining a connection to another process. Cancellation and review decisions are durable requests through the same intake boundary; the driver records and performs their effects. CLI exit stops observation and does not remove accepted work.

### Request acceptance and status

`workflow.Submit` accepts a caller-owned request ID and an explicit `JobSpec`. The ID identifies one logical submission and must be retained across retries. Intake validates the action, destination, verification policy, resolved target descriptions, and source/revision selection. Targets are copied and sorted; target order and nil versus empty variant maps do not change request identity. Different variant selections remain distinct. The first implementation accepts bump, revision-bump, checksum-refresh, verify, and publish requests. Rebase and amend intake remain explicitly unsupported until their input contracts are implemented.

A request against an existing change selects an explicit `InputRevision`; intake does not resolve a moving current-revision default. Such requests omit `Source`. The revision supplies the accepted job's source and change identity, and a supplied `ChangeID` must match. The accepted `JobSpec.Source` is a frozen copy of the revision's source, not another caller-controlled authority. Preparation and publication require the current revision of an open change; verification can select an older revision of an open change. Publication also requires a committed revision. Requests without an input revision supply an immutable source tree and omit `ChangeID`. Intake records the job without creating a change or revision; adoption belongs to later workflow advancement.

Action, destination, and verification policy must be explicit. Preparation may request branch-ready with verification explicitly skipped, verification-complete with verification required, or publication with either explicit policy. Verify requests require verification-complete and verification required. Publish requests require the published destination. An explicit version applies only to bump. Intake performs structural target validation and requires resolved target descriptions; checking that those descriptions match evaluated Portfiles remains part of MacPorts resolution and preparation. It does not discover upstream versions or contact providers or forges.

Under one state transaction, intake checks request identity and referenced records, then writes the queued job and its request receipt. Repository-qualified constraints protect their relationship. Only confirmed commit returns a successful receipt. An identical retry returns the original job ID and acceptance time; reusing the ID with different intent or repository is an error. Compare the immutable accepted spec before checking current change disposition, so later progress or closure does not break a prior receipt. Git validation happens outside this transaction; state writes neither scan nor pin recorded source objects.

`workflow.Status` reads one consistent state view for the selected repository and projects jobs and their related records. Its scope selects all jobs in that repository or a nonempty list of job IDs; combining both, providing neither, or requesting an unknown or foreign-repository job is an error. Repeated IDs are collapsed. A repository-wide result also exposes changes without jobs and independent cleanup obligations. Job-specific reads fetch only related rows. Jobs sort by acceptance time and ID; other collections sort by ID. Publication and PR collections acquire persistence when their executor is implemented.

An absent database or unregistered repository is empty repository-wide status. Corrupt or unsupported state remains an error. Snapshot-read time records when Dockhand read state; it is not a new provider or forge observation. Status never runs a cycle, invokes a provider, registers a repository, migrates a schema, or changes workflow records. Acceptance and status work independently of execution. The existing verification and cancellation behavior below is preserved through the state migration. Verification CLI handlers and current-process residency are now implemented; preparation and publication handlers remain unfinished.

### First verification cycle

`workflow.Engine.Cycle` takes the same explicit all-jobs or selected-jobs scope as status. One pass applies pending cancellation controls, advances at most one external action per selected job, and processes eligible resource releases independently. It does not sleep or wait for a build. The caller supplies later passes. Per-job provider problems are recorded and returned without preventing other selected jobs from progressing; state-store errors or caller cancellation end the pass. `CycleResult.Advanced` lists jobs whose records advanced, not necessarily successful builds. `PendingCleanup` includes retained and uncertain resources as well as requested releases.

Cycles select at most 64 controls, jobs, and cleanup actions each, using indexed queries for eligible controls, jobs, attempts, and cleanup in the selected repository. They skip settled work, live claims, future retries, and retention periods that have not expired. Cancellation reschedules affected work transactionally. Resource cleanup remains independent of the owning job's completion and requires a terminal attempt with established ownership.

Candidate selection grants no ownership. Each handler rechecks current records in a fresh write transaction before claiming work; another driver's claim can make the candidate a no-op. New eligibility appears in later passes. An idle cycle performs reads only and makes no provider calls. The cycle does not construct a public status projection or enumerate all historical records to decide what to claim.

This slice handles `verify` to `verification-complete` for one resolved target and an existing committed input revision. The accepted `JobSpec.Build` records a `BuildConfig`: provider identity, complete platform, immutable environment digest, source-build choice, and explicit test policy. `BuildSpec` combines that configuration with the selected revision, source, target, and concrete artifact inputs. `verify.PlanSingle` constructs this first coverage plan without calling MacPorts or the provider. A missing configuration, missing committed revision, or multiple targets produces a needs-attention outcome. Other action executors remain unimplemented and produce a recorded needs-attention outcome when a cycle encounters them. The broader planner and dependent coverage remain separate future work.

`Submit` validates and copies a supplied build configuration but still accepts requests without one, allowing unfinished preparation paths and previously accepted requests to remain representable. A cycle never fills missing build inputs from current defaults. Admission creates neither a new source revision nor publication authority. Later changes to the tracked change's current revision do not retarget the recorded attempt.

Before submission, the driver records the plan, immutable attempt inputs, submission identity, and an expiring action claim. Capabilities establish provider identity and platform compatibility; advertised capacity is advisory. Only provider `Submit` decides admission. A capacity response leaves the attempt queued without an admission timestamp. Confirmed admission records the provider run, owned resource handles, and first admission time. Submission errors, contradictory responses, and lost claims leave recoverable intent rather than authorizing an immediate duplicate submission.

The first cycle uses two-minute action leases, thirty-second provider call deadlines, and a one-second retry delay by default, configurable on `Engine`. The lease must exceed the call timeout. A live claim excludes other cycles even when they use the same process identity. Each attempt and resource keeps a monotonically increasing claim generation after the active claim is cleared. Recording a result requires the matching owner/generation, a live lease, and the expected current state. Context deadlines are cooperative; a provider must honor cancellation to bound a call's duration. Expiry alone cannot stop a paused process from reaching a provider later.

A canceled or finished observation needs an explicit verdict and observation timestamp. `verify.Judge` rejects contradictory evidence, including a passing summary with failed or unknown steps. The driver validates run identity and rejects observations older than those already recorded. Running evidence remains unknown. Passed verification completes the job; target failure fails it; blocked, errored, or unsupported outcomes need attention. Failed builds are not automatically retried. Transient polling and uncertain effects can be retried on later passes; classified retry budgets and backoff beyond the fixed delay remain future work.

`workflow.Control` accepts idempotent cancellation requests for explicit job IDs. It records intent only. A cycle applies cancellation to its selected jobs; the control's `AppliedAt` means all requested jobs have received that intent or were already terminal, not that remote execution has stopped. A job with no admitted or uncertain submission can cancel locally. An uncertain submission must be reconciled first. For an admitted run, a successful provider `Cancel` acknowledges the request; only a later observation establishes the outcome. Failed cancellation calls alternate with observation so they cannot hide a run that has already finished. An already terminal build retains its factual outcome and diagnostic resources.

## Concurrent execution and external actions

Multiple driver processes may advance the same repository or different repositories in one database. All use the state transaction and claim protocol. Eligibility, claim acquisition, and intended action are recorded atomically; provider, forge, and Git operations happen after commit. Results are recorded under a fresh transaction. Waiting for capacity or a build never holds a database transaction open.

Physical database locking belongs to the backend, while workflow claims are part of the stored records. Do not use an independent lock backend to authorize state changes: the ownership check and mutation must share one transaction boundary. A future executor may need a separate external-resource lock, but that interface and implementation wait for a concrete operation. Drivers coordinating shared external resources must initially use the same database; different database files do not coordinate with each other. Provider admission must still enforce its capacity and stale-call guarantees across repositories.

Claims identify their owner and generation and have explicit liveness and recovery rules. When recording an external result, the driver checks that its claim and the relevant state are still current. An expired or replaced claim cannot authorize a stale process to advance the job.

Claim checks alone cannot undo an external action. Give submissions and publications stable action identities, use provider idempotency where available, and reconcile remote state after an uncertain response or process death. If the provider cannot establish whether an action occurred, retain the uncertainty and report what needs attention rather than blindly creating a duplicate. Apply the same discipline to cancellation and release.

### Verification provider recovery contract

`Submit` must be durable and idempotent by submission ID, enforce capacity at the provider's shared resource scope, and reject reuse of an ID with different build inputs. Its run and resource identities must be usable by another process. `Reconcile` replaces the original lookup-only placeholder: it returns a known run with all recoverable resource handles, durably closes an unadmitted submission, or reports uncertainty.

A `RequestClosed` result is stronger than “not found.” The provider must serialize it against submission and permanently prevent that ID from creating a run, including when a stale driver submits after reconciliation. It must not close an admitted run. Closing may leave partially provisioned resources; return their handles for cleanup. If the provider cannot make this guarantee, it returns `RunUnknown` and the driver does not resubmit.

After confirmed closure with no resources, the driver records the closed identity and allocates a fresh submission ID for the same immutable attempt inputs, unless cancellation is pending. A closure with partial resources ends the job needing attention and schedules those resources for release. Submission IDs remain stable through capacity waiting and uncertain outcomes; they change only after confirmed closure. This prevents an old driver from creating an untracked run after another driver cancels or retries the work. Lost closure acknowledgements are recoverable by reconciling the same ID again.

Provider name identifies a stable recovery namespace. Reusing a provider name for an unrelated backend is invalid. Resource IDs identify unique lifetimes within that namespace and must not be recycled for another attempt. Cancel and release are idempotent operations against recorded identities. A terminal observation means the run has stopped; required evidence and artifact references must remain valid after any permitted environment release.

## Resource retention and cleanup

Resources have records independent of job completion. Record who owns each environment, whether it is active or retained for diagnosis, any retention deadline, and whether release is requested or confirmed.

A dead driver does not prove that its build stopped. An unreachable builder does not prove that it is safe to destroy. Reconcile ownership and provider state before reclaiming resources; a missing or unreadable database is not evidence that every visible resource is unused.

Retention expiry makes an eligible resource due for cleanup; it does not erase an active build or unresolved ownership. Preserve the diagnostic records and required logs before deleting a retained environment. Record release as complete only when the provider confirms release or absence.

A driver retains responsibility for outstanding cleanup even after the requested job outcome is recorded. If cleanup cannot finish, leave a durable obligation with its last error and retry or attention state so another driver can resume it. Independent jobs continue meanwhile.

The first cycle requests release after a passed or canceled attempt and retains failed, blocked, or errored environments indefinitely unless a retention deadline is explicitly recorded. Release uses a separate resource claim, and an unconfirmed release stays uncertain with its last error and next retry time. Provider release is never invoked for an active or unresolved attempt, a foreign handle, or an orphan resource without an owning attempt. Missing ownership is an integrity or reconciliation problem, never permission to release a resource. No automatic retention deadline, retention-management command, or artifact garbage collector is implemented yet.

## Implementation phases and extension boundaries

Phase one implements preparation, verification, publication, and recovery through the shared driver. It establishes stable change identity, explicit revisions, revision-bound evidence, persistent PR association, and separate job and contribution outcomes. Confirming a requested publication belongs to phase one; continuously following reviews and CI after publication can wait for phase two.

### Discovery before a change

Phase two adds `outdated` and may later add scheduled discovery. The upstream discovery and version assessment used by phase-one `bump` must be callable independently of branch creation and job submission. It returns structured observations: source context, current version, eligible candidate version, evidence, observation time, and an update-available, current, or unknown assessment. Failed discovery is unknown, never silently current.

An observation does not itself create a tracked change or authorize work. The standalone command may simply display it. If an observation leads to a bump request, preparation validates its source context and candidate rather than trusting an old discovery result. Automatic latest-version bumps skip current targets and report unknown targets individually; an explicit version request retains its separate meaning.

Discovery does not need a persistent queue, a second driver, or an event bus. Share the discovery service and result vocabulary first; add retained observations only when a consumer needs them.

### Following and revising published changes

Phase two adds ongoing observations of PR state, remote head, CI checks, review decisions, and merge conflicts. Observations carry their time and, where applicable, the revision they concern. Remote CI and review information remain distinct from Dockhand's local verification evidence. Unavailable or stale forge information must remain visibly uncertain.

Monitoring runs through the resident driver and updates the tracked change without holding a completed publication job open. Action invocations run targeted cycles; persistent PR monitoring requires the user to run `dockhand start`. `status` displays the latest recorded information without contacting providers or advancing work itself.

A rebase or correction is another preparation action feeding the existing verification and publication path:

1. Capture the selected local revision, expected remote head, and intended upstream base or explicitly selected edits.
2. Prepare and validate a new revision while preserving the logical commit structure, authorship, and PR association. Conflicts leave recoverable work and an explicit needs-attention result.
3. Verify that revision through the same driver and provider used for initial changes. Preserve older results; reuse them only when the relevant inputs are demonstrably equivalent.
4. If publication was requested, update the associated PR using the expected remote head as a precondition. An intervening maintainer edit or changed PR disposition requires reconciliation, not an unconditional rewrite or a replacement PR.

Corrective edits should be folded into the appropriate logical commit. MacPorts requests squashing follow-up corrections while allowing multiple commits for distinct logical changes; dependent cohorts must not be flattened indiscriminately. See the [MacPorts contribution guidance](https://guide.macports.org/#project.github).

The preparation layer accepts actions beyond initial version, revision, and checksum edits. Exact edit-selection syntax and interactive conflict resolution can be designed in phase two. The core must support an already-existing or externally edited branch as input, without requiring a new bump or a new PR.

Observing a failed check, a review request, or a conflict does not grant permission to change code, rewrite a branch, publish a revision, or merge a PR. Follow-up actions require an explicit request or separately configured authorization. Automated review responses and merging are not prerequisites for either phase.

### Checks that protect these boundaries

As the phase-one core is implemented, verify that one change can retain completed jobs and evidence for multiple revisions; a later publication updates its existing PR association; and a stale job cannot advance a newer revision. Exercise shared discovery without branch or job creation, and verify that job completion is independent of contribution disposition and outstanding cleanup. These checks establish the extension boundaries without implementing the phase-two command handlers early.


## Implemented source binding

`workflow.Engine.BindVerification` binds a literal local branch and an explicit port selection to a commit, tree, target, and build configuration. Git reads and MacPorts evaluation happen outside state transactions. `git.Materialize` creates an owned temporary copy from raw Git objects, including `_resources` and auxiliary files, without checkout filters, archive substitutions, branch movement, or index changes. Missing objects are errors; the current checkout is never substituted. Internal symlinks are supported when they resolve inside the snapshot; external, dangling, cyclic links and submodules are rejected in this first implementation.

`macports.Tree` binds the materialized root, source identity, and evaluation platform. Selecting a target produces a `macports.Context`. Resolution accepts `category/port`, `category/port/Portfile`, or a unique directory name, with an optional explicit subport and variant choices. Broad selectors, arbitrary indexed names, and global subport-name lookup are deferred. MacPorts itself evaluates Tcl; Dockhand does not infer version values from source syntax. A top-level evaluation includes all its subports; an explicitly selected subport evaluates that context alone. Failed subport evaluation rejects the full snapshot. Optional option-read errors remain visible separately from required metadata.

The evaluator starts a fresh MacPorts Tcl session per call and directs source and default resource lookup to the materialized tree. Missing snapshot PortGroups cannot fall back to the installed ports tree. Native platform identity comes from MacPorts: OS platform, OS major version (Darwin major on macOS), and configured build architecture. A requested different or incomplete platform is rejected; cross-platform simulation is deferred. This remains real Portfile execution on the local host, including any host/tool probes that the Portfile performs.

The returned bound request contains source IDs and branch-state preconditions. The temporary directory is removed before binding returns; paths in evaluated diagnostic metadata refer to that temporary observation and are not durable artifact locations. `Submit` transactionally adopts the revision and records the accepted request and job. An unchanged source reuses the current revision; a changed commit creates a successor while retaining prior jobs and evidence. Existing tracked targets must match. If another accepted request advances or closes the change during binding, submission rejects the stale precondition. Git branch movement after binding does not rewrite the already-selected source. Retry the same bound request to recover an uncertain submission; binding again explicitly chooses a fresh input.

No CLI branch-versus-port disambiguation, dirty-worktree policy, branch reassociation command, or automatic adoption scan is introduced by this API. Real Tart verification is now implemented. Verification commands now bind and submit this source; other action commands remain later integration work.

## Prepared-image Tart execution

`verify/tart` now implements the single-target provider contract. SQLite reserves a slot at the canonical Tart-home scope across repositories. Observed external running VMs count toward the configured capacity, although tools outside Dockhand do not participate in its admission transaction. Cooperating drivers must share the database and pool configuration.

Submission records a reservation before cloning a uniquely named VM. An interruption during provisioning is reconciled by stopping and permanently closing that submission, returning any resource for cleanup. After source staging, the provider records admission intent before launching the guest. A later observer can complete that same idempotent launch. Closed IDs permanently reject late submissions; lease expiry is never evidence that capacity is free.

Host launchd runs the VM independently of the submitting driver, and guest launchd runs the build independently of a guest-agent call. These services execute VM/build work only. Workflow progression remains in the current Dockhand invocation or an explicitly started resident driver. The initial native adapter uses the user's GUI launchd domain and a prepared local image; it does not provision images, install host services, or start a hidden workflow driver.

The provider hashes base-image contents and verifies the accepted platform in the guest. It transfers the exact committed Git tree, builds an index from that tree, and sets it as the sole MacPorts source and default resource tree. The guest executes lint, declared tests when requested, and install, recording structured phase evidence and dependency-failure context. No unrelated-dependency failure is automatically attributed to the candidate change.

Terminal evidence and logs are collected onto the host before shutdown. A terminal observation is returned only after the VM is confirmed stopped, and only then is capacity freed. A surviving host result lets a later driver finish this sequence after interruption. Cancellation also checks for a guest that has already finished and preserves its result. VM deletion is separate, idempotent cleanup; required logs/results survive it. An exited runner or stopped VM without a collected terminal result produces an error outcome. A lingering running marker cannot conceal an exited runner.

`app.Build` supplies three-minute provider-call deadlines and five-minute action leases for cloning, image validation, boot, and transfer. These bound driver calls, not the full detached build. Image digest caching is process-local; a fresh process hashes the image again. CLI image selection and current-process resident loops are now implemented. Preparation, dependent scheduling, and publication remain later slices.

## CLI attachment and residency

`verify` binds one committed branch/port input, records the request through `workflow.Submit`, and attaches to its exact job ID. `proc.Manager.Attach` reads selected status, evaluates `workflow.Reached`, and invokes the same cycle used by `proc.Manager.Run`. Default attachment ends at admission or a conclusive result; wait/trace follow the accepted destination. Per-job cycle problems remain recorded and visible while independent progress continues. State errors end the invocation. Resident operation skips status projection entirely and performs indexed cycles until its context is canceled.

Main installs interrupt/termination handling through a cancellation context. Process interruption stops attachment and bounds active provider calls; it does not submit workflow cancellation. Existing claims may need to expire before another process recovers an interrupted external action. `cancel` separately accepts cancellation intent, runs one cycle, and optionally attaches through settlement. No process registry, singleton residency lock, child driver, socket, or operating-system driver service is introduced.

The initial commands use explicit job IDs for reattachment/cancellation. Verification defaults to the current local branch and supports `--branch` for another literal branch; committed source alone is selected. Working-tree snapshots and branch-based target inference are approved above but remain unimplemented; broad selector resolution is also unfinished. Image selection is explicit, and `BuildConfig.ProviderConfig` freezes the provider-specific execution choices in the accepted request. An optional capacity flag establishes shared pool policy; later invocations with no capacity override read its stored value. Empty advertised provider platforms mean validation is deferred to submission, where the exact recorded image, platform, and digest are still checked.

Tracing reads provider logs without changing job state. Bounded chunks go to stderr; JSON results remain a single stdout document. Terminal host logs survive environment cleanup. Status remains an independent read-only command and never runs a cycle or contacts the provider.
