# Dockhand roadmap

This document is the current source of truth for implementation priorities. The [architecture](architecture.md), [component map](components.md), [CLI design](cli-design.md), and [state design](state.md) define behavior and boundaries. Activity reports retain implementation history and validation; they are not additional queues.

Last reconciled: 2026-09-16, after contribution lifecycle and [routine cleanup](activity/2026-09-16-routine-cleanup.md). **Next** is ordered. Later capabilities and maintenance work are not prerequisites unless stated explicitly.

## Next

### 1. Improve platform observations and context selection

Continue [bump coverage stage 2](bump-coverage.md#2-improve-platform-observations-and-context-selection). Fetch-guard classification and planning before dependency downloads are complete; scalar/option thresholds and non-conditional OS reads are not.

Have `macports/eval` expose source-bound observations; keep context selection in `macports/portedit`. Resolve supported thresholds through native evaluation, retain the source scanner for unvisited branches, and report unresolved coverage explicitly. Preserve independent older-OS release/checksum pins.

Acceptance: the py-openssl/mrustc threshold cases and libfec/mpir formatting reads, plus mutable thresholds, candidate-activated branches, and unresolved dimensions. Measure session counts and timings while retaining the existing literal-boundary and archive controls. Several modeled profiles are not proof of arbitrary Tcl or build compatibility.

### 2. Separate manifest-bearing source from auxiliary archives

Implement [bump coverage stage 4](bump-coverage.md#4-identify-dependency-source-archives-independently-of-auxiliary-files) before expanding contribution scope. This is a bounded single-target improvement and does not depend on shared-subport publication.

Identify one unambiguous source for Cargo/Go manifests while retaining independently pinned auxiliary archives. Codex's Cargo source plus pinned V8 archive is the concrete control. Keep archive ownership in `macports/distfiles`, manifest/helper validation in `macports/dependency`, and orchestration in `portedit`. Preserve baseline generator comparisons and maintained overrides; do not pick the first archive or infer ownership solely from its filename.

Acceptance: complete candidate preparation with the auxiliary pin unchanged; ambiguous ownership, missing manifests/helpers, and unsupported extraction remain actionable refusals. Verify the prepared target using the ordinary contribution workflow.

### 3. Carry shared-release subports through the whole workflow

Implement [bump coverage stage 3](bump-coverage.md#3-represent-a-shared-release-across-subports-end-to-end) after the smaller preparation improvements. Named subport lookup is complete; authorizing one release to change several subports is a different capability.

Represent affected and protected targets explicitly using declaration provenance and before/after source evidence. Assessment/preview must explain the enlargement and let the user accept its scope. Carry it through intake, revisions, correction, restart, evidence reuse, isolated verification, and publication together. Reuse the existing per-target verification plans where their contracts fit.

Acceptance: py-memprof/py-ipdb shared releases, protected Terraform/Helm series and libusb-devel pins, conflicting targets, and cancellation/restart. Every required target must pass before publication. Shared-release siblings are not reverse dependents; deleting sibling-fidelity checks or setting `IncludeDependents` is not an implementation.

## Later capabilities

### Broader PR observation

After the basic lifecycle work, observe PR head, mergeability, review, CI, and conflicts. Publication still completes when the PR is opened or updated; later observations attach to the contribution. Observation does not authorize automatic corrective edits, pushes, or responses to reviewers.

### Dependent verification follow-up

Direct-dependent discovery, isolated Tart builds, per-target images, and full-cohort publication checks are implemented. Remaining work includes baseline comparison when an unrelated dependency causes a failure, a separate design for automatic downstream revision edits, and GitHub dependent coverage. Keep those separate from shared-release subports.

### Broader selectors and multi-target intake

Names and indexed subports work now; `assess` and `outdated` also support maintainer/category selection, and `assess` can scan the tree. Batch preparation remains future work: expose one result per requested target and preserve independent failures, admission rules, and attachment behavior. Do not add a generic batch interface merely to implement one shared release.

### GitHub verification refinements

Consider controlled reruns, safe updates of already-pushed verification branches, and broader workflow coverage. After a local branch rename, verification currently uses the new local branch while publication retains the existing PR head; coordinating that identity is a concrete follow-up. Shared-run cancellation, missing-run diagnostics, delayed-run recovery, offline cancellation, and log-cache retention already exist; see the [GitHub exercise](activity/2026-09-15-xplr-github-exercise.md).

### Expiring credentials

Add access/refresh-token storage, expirations, and cross-process refresh rotation before enabling an authentication flow that requires expiring user tokens. Preserve the selected identity and require login again only after revocation or unusable refresh credentials. This is separate from the implemented device login and registered OAuth client.

### Further preparation coverage

Triage the survey's weak/missing digest findings separately from checksum ownership ambiguity. A future checksum-upgrade operation should compute and add a strong digest with correct source ownership; simply removing the SHA256 requirement is not the fix. Arbitrary livecheck scripts, multiple independent version inputs, new release-series creation, and coordinated Rust/bootstrap maintenance require separate designs rather than per-port special cases.

## Needs design

### Review and unattended-publication authority

`review accept` and `review dismiss` still need defined durable meaning: what creates a pending review, what acceptance authorizes, how it binds to a revision, and what dismissal closes. A manual bump/publish already records direct user intent. Implemented contribution abandonment is separate from a future review system.

Before discovery or a persistent driver can originate publishable work, define requester provenance and authority. Read-only `outdated` and starting a driver must not silently grant publication permission.

### More permissive human-edit capture

Managed amend/rebase and explicit reassociation are implemented under the [human-correction contract](human-corrections.md). Adopting unstaged tracked corrections or rebasing a checked-out branch needs crash-safe index/worktree preservation. Existing conservative preconditions remain until that design is complete.

Standalone `publish` continues to require explicit verification when evidence is missing. Combined correction-and-publish commands authorize both steps; this is a settled rule, not an unimplemented feature.

### Evidence across repository registrations

One database supports multiple repositories, but evidence is currently scoped to one registered clone. Decide whether identical tree/build inputs may share evidence across registrations and what repository trust checks are necessary. This is distinct from preserving identity within one contribution.

## Maintenance and regression work

These are ongoing checks or evidence-triggered investigations, not unfinished implementations of their existing capabilities.

- **MacPorts compatibility:** native evaluator tests pass on Base 2.12.6 and an isolated Base 2.11.6 on Darwin 25 arm64. Extend runtime coverage when needed, keeping Base and PortGroup compatibility distinct. Add version-specific adapters only for demonstrated differences; see [compatibility evidence](macports-compatibility.md).
- **Provisioning reliability:** all ten tested profiles were independently checked, including two successful Monterey/Xcode 14.2 provisions. Earlier first-boot exit 125 and Monterey extraction failures did not recur; their original causes remain unestablished. Preserve stage-specific evidence if they recur. Long-stage progress, readiness handling, failed-guest cleanup, and replacement rollback are implemented; see the [reliability exercise](activity/2026-09-15-provisioning-reliability.md).
- **Tests and package boundaries:** preserve the useful integration boundaries from the [review 7 follow-up](activity/2026-09-15-review7.md). Extend focused import checks when touching meaningful dependency boundaries; do not split packages by file count or duplicate workflow engines.
- **Exported surface and documentation:** audit Tcl/upstream exports against real callers and protocol use. Package overviews exist; improve operation/recovery contracts when useful. Keep implementation history in activity reports and current ownership in `components.md`; review raw benchmark retention without automatically deleting history.
- **Caches and transport:** preparation precedes Tart capacity reservation; exact candidate indexes, incremental index reuse, shared `fetch`, and explicit cache retention are implemented. Keep cache ownership in callers. The cold manual-branch Terraform cleanup exercise generated a full index twice, in discovery and Tart staging (about four minutes each); investigate safe reuse for identical source/tool/platform inputs across those cache roots before adding another cache abstraction. Reconsider splitting PortIndex query/staging only for a concrete consumer boundary.
- **Rate limits:** consider account-wide GitHub cooldown coordination only if concurrent measurements justify it beyond the existing persisted per-record deadlines.

## Completed capabilities

Extend these through their existing paths rather than treating them as new roadmap items:

| Capability | Established behavior / evidence |
| --- | --- |
| Target-based contribution workflow | Exact-source port/subport resolution; early contribution identity; transactional acceptance, replay, and frozen retries; named verify/publish/status/wait/cancel; explicit manual checkout verification. [Integrated validation](activity/2026-09-16-target-workflow-validation.md). Local abandonment and explicit PR refresh retire settled contributions while preserving corrections and history. [Lifecycle validation](activity/2026-09-16-contribution-lifecycle.md). |
| Release discovery and assessment | GitHub/GitLab catalogs and supported native HTTP regex livechecks shared by bump/outdated; local assess with optional candidate probes; maintainer/category selectors and whole-tree assessment. Terraform selected 1.16.3 and passed Tart verification/publication preview. |
| Source preparation | Evaluator-guided calculated versions, revision bumps, checksum refresh, scoped series updates, conditional/multiple archives, preserved pins, and supported Go/Cargo regeneration. Rejection-only fetch guards and local planning before helper/download work are implemented; Wasmer 7.4.2 passed preparation and Tart verification. |
| State and recovery | Repository-scoped SQLite, concurrent claims, cancellation/recovery, explicit phases, immutable source capture, backup/check/migration, and durable snapshots. Schema-15 migration was rehearsed on a copy of existing user state without losing history/evidence. |
| Verification and publication | Tart preference with GitHub fallback, immutable image/evidence inputs, reuse, GitHub fork verification, durable publication recovery, and independent per-target dependent builds with full required coverage. Local exercises in this milestone used publication previews; earlier GitHub exercises reached live PRs. |
| Setup and credentials | Base/full-Xcode Tart provisioning and capacity-aware image inspection; device login, credential precedence/diagnostics, registered OAuth client, and public-read authentication. |
| Corrections and operations | Managed amend/rebase, branch reassociation, conditional PR updates, resource release, manual gc and cache retention, durable logs/progress, tested Tcl transport, and package documentation. Automatic failed-VM release, explicit `--keep-failed`, prompt transfer-archive removal, and bounded expiry of released diagnostics are implemented. [Cleanup validation](activity/2026-09-16-routine-cleanup.md). |

## Deferred

- Exporting state to Git notes or reconstructing the database from Git metadata.
- Portable verification-evidence exchange between machines.
- A generic workflow DAG or package-build scheduler.
- Automatic mutation in response to PR reviews, CI failures, or merge conflicts.
- Broad provider matrices and an `all`-platform execution mode.
- A QEMU provider without a concrete current use case.
- Supporting every v1 command or mechanism without a current use case.

## Review triage and validation

Earlier findings about whole-state Git-ledger writes, SQLite migration structure, repeated phase inference, state/workflow ownership, CI, workflow organization, shared Tart mechanics, and the unused placeholder planner have been addressed. Remaining useful review findings are represented above. Do not reintroduce the discarded Git ledger or require v1 feature parity as a prerequisite for this queue.

End meaningful milestones with targeted user-path and recovery exercises. For coverage changes, replay the pinned 147-Portfile corpus and the targeted controls, distinguishing input discovery, candidate checks, actual archive preparation, and builds. Preserve Terraform/Helm/gh/Deno and the new Wasmer path as controls. Repeat a provisioning matrix or create live PRs only when the changed behavior warrants it. Historical exercises and reviews are evidence, not additional queues.
