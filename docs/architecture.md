# Architecture

This document applies the [design principles](principles.md). The [CLI design](cli-design.md) specifies command names, attachment behavior, and user-visible results. The proposed [component structure](components.md) maps these responsibilities to Go packages.

## Configuration

Dockhand uses the user's global Git configuration for defaults. Repository Git configuration can override those defaults, and explicit command options take precedence for the submitted request.

Accepted jobs record the effective choices needed to explain and reproduce their work. A later configuration change must not silently change an accepted job's source, build question, or requested destination. Credentials remain outside durable job records.

## The Ledger

Dockhand uses Git for its authoritative workflow records, without a separate database service or database file. A dedicated ref, `refs/dockhand/state`, holds structured records for tracked changes, their revisions, jobs, attempts, resource ownership, and publication. These records live in the same repository as the change branches. Linked worktrees share the ledger through the Git common directory.

Git notes expose commit-associated summaries derived from the authoritative records. They can be regenerated and are not consulted to decide whether to submit, publish, or release a resource. Verification records are durable evidence, not disposable caches.

Logs, downloaded sources, build artifacts, and VM images may live outside Git. The ledger records their identities, locations, and retention obligations where needed. Losing an artifact must not erase the recorded outcome; where that artifact is required for further work, its absence prevents reuse.

Ledger mutations use a common lock in the Git common directory and compare-and-set ref updates. Related records and branch or pin ref changes that must land together are committed in one ref transaction. Records that refer to source objects must keep those objects reachable for as long as recovery or reuse requires them; a SHA written inside JSON is not itself a Git reachability guarantee.

### Snapshot reads and write transactions

The initial ledger format is a schema-versioned `state.json` blob in the tree of each state commit. `Read` captures the state ref once, then reads the commit, tree, and document using immutable object IDs. It returns a caller-owned `Snapshot` with the captured commit ID as its version. Reads do not acquire or create the writer lock, query providers, or update records. A missing state ref is distinct from unreadable objects, malformed records, and an unsupported schema.

`Update` acquires an advisory lock at `$GIT_COMMON_DIR/.dockhand-ledger.lock`, reads current state, invokes its mutation callback once, writes the new objects, and commits guarded ref changes. It never automatically replays a callback. The previous state commit is the new commit's parent; an unchanged state without ref effects does not produce another commit. The ledger commit uses a Dockhand identity independent of the user's commit identity. Git plumbing uses command-local hook, signing, and fsync settings without changing repository configuration or the checkout.

The callback receives a transaction context and the current records. Callbacks run synchronously and must only validate and mutate state. Preparation, provider calls, publication, and other external work happen outside the transaction. Nested writes using the transaction context are rejected. Carry that context into calls made from the callback; replacing it with an unrelated context bypasses this nesting check and may cause a bounded lock wait.

Default lock acquisition is bounded to five seconds, and a read or write operation has a thirty-second context deadline; callers can configure these bounds or supply an earlier deadline. Cancellation does not forcibly terminate arbitrary Go callback code or uninterruptible filesystem operations. The lock remains held until the callback returns or unwinds, and an expired callback cannot proceed to commit. A process exit releases the advisory lock; never delete the lock file to bypass an active holder.

Each update guards the state ref and any related ref changes with their expected prior values. Ref precondition failures are returned to the caller for an explicit fresh decision. Cancellation or abnormal process termination during the final Git command can leave an uncertain commit outcome; callers must inspect the ledger, using durable request/action identities, before retrying. Git's own leftover lock files are separate from the advisory writer lock and are not automatically removed by Dockhand.

A successful ref transaction coordinates changes against writers, but concurrent readers do not receive an atomic view across separate refs. The ledger snapshot and its recorded source object IDs are authoritative for that read; independently inspecting a live branch is a separate observation. See [Git's ref transaction semantics](https://git-scm.com/docs/git-update-ref).

### Source retention

The ledger creates `refs/dockhand/objects/<object-id>` pins for local source commits, base commits, source trees, and desired publication heads named by persisted records. It checks object types in a batch and creates or verifies the pins in the same ref transaction as the state update. Source retention uses actual Git refs, not object IDs embedded in JSON. Observed remote heads are not automatically treated as locally available source objects.

Pins and ledger history are retained indefinitely in this implementation, including after records stop referring to a source. There is no compaction or pin deletion yet. Any future retention policy must account for revision history, recovery, evidence reuse, and readers finishing older snapshots.

## Changes, revisions, and jobs

A tracked change represents the logical contribution. Give it a stable identity independent of a branch name, commit SHA, job, or PR number. It relates the affected ports, current local revision, jobs, and any associated PR. Its lifetime extends beyond any individual execution job.

A revision identifies an immutable source snapshot and its upstream base. Preparation records how a new revision relates to its predecessor. Rebasing or incorporating corrective edits produces a new revision of the same tracked change. Rewriting commits must preserve the change identity and PR association, along with the earlier revisions and evidence needed for history and recovery.

A job is one accepted request against that change: prepare it, verify a selected revision, or publish a selected revision. Record its input revision and any resulting revision explicitly. Each follow-up request has its own outcome; it does not reopen a completed job or silently retarget an active build to a moving branch. An initial preparation job may create the change and its first revision.

Publication has two separate concerns: an action that opens or updates a PR, and the continuing association with that PR. Keep the PR identity, last confirmed published revision, current local revision, and latest observed remote head distinct. A publication job finishes when its requested revision and metadata have been confirmed on the forge. It does not wait for merge. The tracked contribution can remain open through further review, revisions, and jobs, until its disposition is recorded as merged, closed, or abandoned.

A later branch edit does not inherit authorization to publish from a completed job. Likewise, an older verification finishing after a new revision is prepared records evidence for its own inputs; it cannot silently publish the older revision over the new one. Serialize mutations to a change and check expected local and remote heads before applying them. Independent builds can continue against their pinned inputs.

These are domain distinctions, not a requirement for separate services, packages, or a generic workflow engine. Plain records with explicit identities and relationships are sufficient. Establish them in phase one even where the user-facing actions arrive in phase two.

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

The user explicitly starts persistent mode with `dockhand start`. It repeatedly processes eligible repository work through the same workflow implementation. Ongoing PR monitoring is a phase-two extension of this mode. Ledger transactions and claims coordinate concurrent action invocations and persistent execution; starting a driver grants no additional publication authority.

Once an action invocation exits, further workflow advancement requires an already-running persistent driver or another invocation that runs driver cycles. Submitted provider builds may continue independently, while settlement, publication, and cleanup remain durable obligations for later cycles.

**Driver ownership and job milestones**

A job is the driver's durable record of an accepted operation. It names the target branch or revision, the requested work and destination, and the verification or publication work needed to get there. It may require several provider builds, including downstream dependent builds. It is not synonymous with one VM or one provider request.

| Milestone | Meaning |
| --- | --- |
| Accepted | The shared workflow submission API has durably recorded the queued job in the ledger. The CLI receives its job ID; driver pickup may happen afterward. |
| Admitted | The verification provider has accepted the initial build request. Persisting a local queue entry does not count as provider admission. |
| Completed | The requested destination has been reached and its outcome recorded. Publication completion means the PR was opened or updated, not that it was merged. |

A refusal, failed verification, or condition requiring attention is reported explicitly when the requested destination cannot be reached. It must not be displayed as successful admission or completion. A job can also complete from matching existing evidence without starting another build.

After durable submission, the driver owns status recording, capacity waiting, provider submission, evidence collection, retries, publication, environment retention/release, and recovery bookkeeping. Submission and driver pickup are separate: a recorded request is recoverable even if no driver has claimed it yet. The CLI must not independently poll the provider to decide a verdict, settle a build, or release an environment. It displays the driver's recorded state and progress.

## Ledger-based request handoff

The ledger carries requests from command invocations to driver cycles and carries recorded progress back to observers. There is no Unix socket or separate local request transport. The CLI invokes shared `workflow` intake functions to persist requests transactionally, rather than constructing or mutating workflow records itself. Resident and targeted drivers read and claim eligible work from that same ledger.

The accepted milestone means the durable intake transaction succeeded. Driver pickup is a later claim, and provider admission remains a separate milestone. Starting or finding a process proves neither. An interrupted submission can be looked up by its request ID. Exiting the invoking process leaves the recorded request available for recovery by a later cycle.

`wait` and the waiting portion of other commands observe ledger records and may run targeted cycles through the shared workflow engine in the same process. Here, attachment means remaining present to observe progress, not maintaining a connection to another process. Cancellation and review decisions are durable requests through the same intake boundary; the driver records and performs their effects. CLI exit stops observation and does not remove accepted work.

## Concurrent execution and external actions

Multiple driver processes may advance the same repository. All use the same ledger transactions and claim protocol. A process claims eligible work and records the intended external action under the ledger lock, then releases the lock before staging, provider calls, forge calls, or other long-running work. No driver holds the global ledger lock while waiting for capacity or a build.

Claims identify their owner and generation and have explicit liveness and recovery rules. When recording an external result, the driver checks that its claim and the relevant state are still current. An expired or replaced claim cannot authorize a stale process to advance the job.

Claim checks alone cannot undo an external action. Give submissions and publications stable action identities, use provider idempotency where available, and reconcile remote state after an uncertain response or process death. If the provider cannot establish whether an action occurred, retain the uncertainty and report what needs attention rather than blindly creating a duplicate. Apply the same discipline to cancellation and release.

## Resource retention and cleanup

Resources have records independent of job completion. Record who owns each environment, whether it is active or retained for diagnosis, any retention deadline, and whether release is requested or confirmed.

A dead driver does not prove that its build stopped. An unreachable builder does not prove that it is safe to destroy. Reconcile ownership and provider state before reclaiming resources; a missing or unreadable ledger is not evidence that every visible resource is unused.

Retention expiry makes an eligible resource due for cleanup; it does not erase an active build or unresolved ownership. Preserve the diagnostic records and required logs before deleting a retained environment. Record release as complete only when the provider confirms release or absence.

A driver retains responsibility for outstanding cleanup even after the requested job outcome is recorded. If cleanup cannot finish, leave a durable obligation with its last error and retry or attention state so another driver can resume it. Independent jobs continue meanwhile.

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
