# Dockhand roadmap

This document is the current source of truth for implementation priorities. The [architecture](architecture.md), [component map](components.md), [CLI design](cli-design.md), and [state design](state.md) define behavior and boundaries. Activity reports record completed work and its validation; they do not maintain the current queue.

The order within **Next** is intentional. Other sections describe accepted direction, unresolved design, or explicitly deferred scope without promising implementation order.

Last updated: 2026-09-16.

## Next

### 1. Keep one target through bump, verification, and publication

The [target workflow plan](target-workflow.md) is the immediate implementation sequence. The Terraform exercise showed that supported explicit-version editing does not yet imply automatic discovery or a continuous contribution workflow. A failed bump was followed by a successful standalone verification of unchanged master; no update branch existed.

Implement in this order:

1. Make outcomes distinguish failed preparation, standalone verification, an actual prepared update, and publication readiness.
2. Unify source-bound port/subport name resolution across commands and remove public `--subport` once it works everywhere.
3. Record the contribution when preparation is accepted; keep retries under it, with transactional selection, migration, and interrupted-integration checks.
4. Let `verify <target>`, `publish <target>`, and target-oriented status follow that contribution. Make checkout verification explicit, handle ambiguity, and preserve revision/evidence checks.
5. Add evaluated HTTP regex livecheck discovery alongside forge catalogs, shared by bump/outdated. Terraform should select the newest eligible release in its selected series without an explicit version.
6. Complete Wasmer's fetch-guard/local-planning reliability slice described below, then exercise the integrated paths.

The plan defines stage boundaries, package ownership, retry/no-update behavior, human edits, concurrency, and acceptance checks. It is proposed behavior, not a claim that the commands have changed. One focused `macports/selection` package is justified; no external dependency or second workflow engine is required. PR observation and expiring OAuth tokens remain behind this core work.

### 2. Continue broader Portfile coverage

The [bump coverage plan](bump-coverage.md) and its [readiness investigation](activity/2026-09-16-core-bump-readiness.md) remain applicable. Its first implementation slice is included in the target-workflow milestone: distinguish rejection-only fetch guards from archive modifications, report the affected context, and finish local candidate planning before dependency-source downloads/helpers.

Then implement:

1. Improve platform observations and context discovery for scalar/option thresholds and non-conditional OS reads without dropping unresolved coverage.
2. Support one shared release across subports with explicit contribution scope, preserved pins, and verification/publication coverage for every required target.
3. Allow dependency regeneration with one identified manifest-bearing source plus independently pinned auxiliary archives; this can proceed independently of shared-subport publication.

Keep native semantics in `macports/eval`, planning in `macports/portedit`, archive ownership in `macports/distfiles`, and helper/manifest validation in `macports/dependency`. The initial [bump planner](bump-planner.md), [scoped implementation](activity/2026-09-15-scoped-bump-planner.md), and [hardening pass](activity/2026-09-15-planner-hardening.md) are implemented. Existing Terraform/Helm series, gh source/binary branches, and Deno architecture archives have passed preparation exercises; retain them as controls, without equating those exercises with complete automatic bump support. New release-series creation and coordinated Rust/bootstrap maintenance remain outside this pass.

Source/dependency preparation, GitHub verification, automatic Tart preference, per-target dependent images, and maintainer/category discovery are already implemented. Baseline comparison for dependent failures and automatic downstream revision edits remain separate work; GitHub dependent verification remains unsupported. Keep exercise evidence in activity reports and unresolved work here.

## Planned

These items have a useful place in the current architecture but are not the immediate implementation queue.

### Broader selectors and multi-target intake

Extend the current single-port selector deliberately. A multi-target request should expose one result per target, preserve independent failures, and use the same admission and attachment rules as current jobs.

### Upstream discovery

`outdated` is implemented for explicit selectors, including GitHub/GitLab catalogs and calculated-version probing. Unknown or incomplete observations remain visible. Maintainer/category selection is implemented; discovery does not create branches, jobs, or publication authority.

### Preparation assessment

`assess` is implemented for explicit ports, maintainer/category selectors, and whole-tree scans. Optional `--version` checks a resolved release with shared preparation checks; local assessment does not query upstream. Results distinguish editable inputs, checked candidates, missing helpers, known limitations, and uncertainty. See [CLI behavior](cli-design.md#preparation-assessment).

### Pull-request observation

Teach resident driver cycles to refresh PR head, mergeability, review, CI, and conflict observations. Publication still completes when the PR is opened or updated; later observations remain attached to the contribution.

### MacPorts Base compatibility and fetch semantics

Capability checks and host Base/Tcl diagnostics are implemented. Native evaluator tests pass on Base 2.12.6 and an isolated Base 2.11.6 build on Darwin 25 arm64. Earlier source-reviewed releases remain source-review evidence only; see [compatibility scope and reproduction](macports-compatibility.md).

- Extend runtime coverage to additional Base/OS combinations when needed, tracking PortGroup compatibility separately.
- Keep metadata access, fetch registration, and Go hook recognition within the shared MacPorts adapter. Introduce version-specific adapters only for demonstrated incompatibilities; unknown versions are not rejected by version number alone.

### Provisioning follow-up

The [fresh provisioning exercise](activity/2026-09-15-fresh-provisioning.md) initially passed nine of ten profiles. The [reliability exercise](activity/2026-09-15-provisioning-reliability.md) subsequently provisioned Monterey/Xcode 14.2 twice and independently checked all ten profiles.

- Earlier first-boot exit 125 failures on Sonoma/Sequoia did not recur in the new live runs. Registration now observes launchd domains/services, tolerates observed registration races and transient domain rejection, and reports bounded readiness failures. Preserve stage-specific evidence if the earlier failure recurs; successful runs do not establish its original cause.
- Monterey/Xcode 14.2's earlier native extraction failure also did not recur with the same archive and source-image digest. Both host extraction and two fresh guest installations succeeded. Its original cause remains unestablished; do not describe it as a proven archive, memory, disk-space, or concurrency defect.
- Long-stage progress, bounded SSH handshakes, prompt failed-guest cleanup, and image-replacement rollback are implemented and covered by regressions/live exercises. Continue targeted failure exercises when these mechanisms change.

### Engineering follow-up

- Source archives are now prepared before Tart capacity reservation. Exact candidate indexes are retained, and standalone edits can seed from a cached tree using the complete Git diff. Index-cache retention is implemented; the [retention smoke exercise](activity/2026-09-15-retention-smoke.md) measured a cold index build and confirmed warm reuse before admission.
- Integration suites were remeasured serially without a live VM exercise in the [review 7 follow-up](activity/2026-09-15-review7.md). Keep native snapshot/storage integration tests with preparation; relocate duplicated CLI scenarios only when a concrete responsibility boundary warrants it. Retain parsing, rendering, exit-code, wiring, and recovery coverage.
- Focused import checks now protect `verify` and `outdated`. Extend automated checking to the remaining meaningful dependency rules in `components.md` when those boundaries are touched; avoid a broad stylistic lint regime.
- Audit unused exported Tcl/upstream APIs and reserved scaffolding against current callers and protocol use. Unexport, remove, or test deliberately; do not delete functioning planning code based on an older review's inventory.
- Package overviews now cover every package, including the workflow file-group map. Continue documentation of exported operations and recovery contracts where it aids callers, rather than targeting comment counts.
- Keep `components.md` focused on the current map, responsibilities, and dependency rules. Link to activity reports for implementation history instead of repeating it. Review how raw benchmark data is retained while preserving reproducible commands and useful conclusions; no automatic deletion of history is implied.
- PortIndex and GitHub log-cache retention is implemented in `gc`, with last-use thresholds, existing operation locks, read-only previews, and retained database evidence. Consider orphaned temporary-file cleanup separately; unidentified/incomplete temporaries are currently preserved.
- Add expiring OAuth token support before enabling it for Dockhand's registration: retain access and refresh tokens with their expirations, coordinate refresh-token rotation across processes, and require login again only after revocation or an unusable refresh token.
- Consider account-wide GitHub cooldown coordination only if measurements show concurrent jobs continue causing rate-limit pressure despite their persisted per-record deadlines.
- The shared `fetch` package now serves bounded source-archive and PortIndex transfers; keep cache/storage ownership in callers.
- Reconsider separating PortIndex reading/querying from staging/cache maintenance when extending selectors. The seam is credible, but current consumers stage then read; no immediate split is required.

## Needs design

These items should not be implemented from their existing command placeholders alone. Build post-publication commands on the implemented correction contracts; settle review authority and requester provenance before unattended discovery can originate publishable work.

### Review controls

The design names `review accept` and `review dismiss`, but it does not yet define what creates a pending review, what acceptance authorizes, or what dismissal closes. A manual `bump` or `publish` already records direct user intent. Review controls should be designed with discovery, unattended work, and corrective edits so their durable consequences are unambiguous and revision-bound.

At minimum, the design must settle:

- whether review applies to a discovered candidate, a prepared revision, or publication authority;
- how a user inspects the exact diff and evidence before deciding;
- how acceptance is bound to the current revision and rejected after it changes;
- whether dismissal closes one revision, the whole contribution, or only pending jobs; and
- how the driver reports and applies the decision idempotently.

### Human corrections and post-publication work

The accepted [human-correction design](human-corrections.md) is implemented with conservative checkout preconditions. A future extension may adopt unstaged tracked edits or update a checked-out rebase while preserving index/worktree intent across crashes. GitHub verification after a local rename currently uses the new local branch, while publication retains the original PR head; coordinating that remote branch directly is a later refinement.

### Standalone publication with missing verification

Decision: standalone `publish` keeps requiring explicit `verify` when evidence is missing. Combined correction-and-publish commands express authority for both steps.

### Requester and unattended-publication policy

Before discovery or persistent drivers can originate publishable work, define which provenance and policy inputs must be durable. Starting a driver must not silently grant publication authority.

### Evidence across repository registrations

The shared database currently scopes verification evidence to one registered clone, even when another clone contains the same tree. Decide whether identical tree and build inputs may share evidence across repository registrations and what repository trust checks that requires.

## Deferred

- Exporting workflow state to Git notes or reconstructing the database from Git metadata.
- Portable verification-evidence exchange between machines.
- A generic workflow DAG or generic package-build scheduler.
- Automatic mutation in response to PR reviews, CI failures, or merge conflicts.
- Broad provider matrices and an `all`-platform execution mode.
- A QEMU provider without a concrete current use case; it remains an architectural thought experiment.
- Supporting every command or internal mechanism from Dockhand v1 without a current v2 use case.

## Completed foundations

The following capabilities are established and should be extended through their existing paths:

- repository-scoped SQLite state shared safely by concurrent driver processes;
- explicit job phases, transactional claims, recovery, cancellation, and resource cleanup;
- workflow lifecycle organization that keeps binding, intake, policy, execution, and projection roles visible without exporting driver internals;
- immutable committed and working-tree source capture with native MacPorts evaluation;
- native credential-applicability checks that block unsupported authenticated source downloads without exposing secrets;
- named and multiple source checksums, preserved local patches, and guarded Go/Rust dependency regeneration through optional host helpers;
- Tart verification with shared capacity, result reuse, retained diagnostics, and garbage collection;
- base and full-Xcode Tart provisioning through `setup`, with automatic profile selection;
- capacity-aware validation of provisioned and custom Tart images, with immutable-digest caching and reusable environment evidence;
- tested Tcl subprocess/RPC failure contracts, strict reply framing, pre-dispatch cancellation, and handshake deadlines covering script loading;
- evaluator-guided calculated-version probing and standalone checksum refresh through `macports/portedit`;
- claimed direct-dependent discovery, durable isolated coverage plans, full-cohort publication checks, and live conflicting-dependent/failure exercises;
- managed amend/rebase, explicit branch reassociation, and conditional existing-PR updates preserving local/remote identities;
- read-only `outdated` discovery for explicit port selectors;
- GitHub fork verification through publication, shared-run tracking cancellation, durable progress, and resumable/offline log reads;
- consistent public-read authentication, publication rate-limit recovery, and durable failure backoff distinct from expected waiting;
- automatic bump selection of a suitable prepared Tart image, with GitHub fallback only for availability conditions; explicit choices remain authoritative;
- status as a durable snapshot, distinct from driver reconciliation and external observation.

## Review triage

This ordering incorporates the findings that remain useful from Claude's four project reviews and workflow review. Earlier findings about whole-state Git-ledger writes, the SQLite migration ladder, repeated phase inference, state/workflow policy ownership, missing CI, workflow file organization, shared Tart mechanics, PortIndex placement, and the unused placeholder planner have already been addressed; they are not new pending work.

The remaining test-placement, mechanism-documentation, exported-surface, and dependency-checking suggestions are represented above; the status contract is now explicit in the principles. Cross-repository evidence reuse, requester provenance, review controls, and PR observation retain their existing design/planning slots. Do not split workflow merely because it is large, reintroduce the discarded Git ledger, rename the user-selected environment variables, or require v1 feature parity as a prerequisite for this queue.

## GitHub provider follow-ups

The [xplr exercise](activity/2026-09-15-xplr-github-exercise.md) completed fork verification through PR, driver recovery, shared-run tracking cancellation, and a user-triggered rerun.

- Missing-run observations now identify the accepted source, submission time, push condition, and current workflow settings; wait/cancel guidance and delayed-run/offline-cancellation regressions are implemented. See the [recovery guide](github-verification.md#when-a-pushed-branch-has-no-visible-run). Automatic reruns and managed branch updates remain below.
- Consider controlled rerun support, safe updates to previously pushed branches, and broader cohort/workflow coverage after the initial committed single-port path.
- Log-cache retention is tracked with engineering storage follow-up above.

## Validation milestones

End each meaningful milestone with a targeted user-path exercise or recovery regression. Repeat a full provisioning matrix or create live PRs only when the change warrants it. Historical exercise reports and reviews remain evidence, not additional queues.
