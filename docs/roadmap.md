# Dockhand roadmap

This document is the current source of truth for implementation priorities. The [architecture](architecture.md), [component map](components.md), [CLI design](cli-design.md), and [state design](state.md) define behavior and boundaries. Activity reports retain implementation history and validation; they are not additional queues.

Last reconciled: 2026-09-16, after the [new-user deno exercise](reviews/2026-09-16-new-user-deno-exercise.md) and the [PortIndex storage design](portindex.md). **Next** is ordered. Later capabilities and maintenance work are not prerequisites unless stated explicitly.

## Next

### 1. Refuse publication pushes to repositories the user does not own

Implemented on 2026-09-16; see the [activity report](activity/2026-09-16-publication-owned-head.md). The README promised that nothing is pushed to `macports/macports-ports` directly, but destination resolution selected the push remote by name only. A checkout whose `origin` is the upstream repository, with the fork under another remote name, planned to push the contribution branch upstream. Destination resolution and frozen-destination planning now require the push repository to be owned by the authenticated GitHub login and name the remotes that are.

### 2. Consolidate PortIndex storage and reuse

Implemented on 2026-09-16 per the [PortIndex storage design](portindex.md); see the [consolidation report](activity/2026-09-16-portindex-consolidation.md). One shared cache root holds immutable generations per indexing environment; candidates derive from their base, master advances incrementally from the retained seed, and every consumer reuses the same generation. The cold deno revision bump fell from 9m58s to 6m38s with one full pass instead of two. Both follow-ups are done; see the [follow-up report](activity/2026-09-16-portindex-follow-ups.md): package tests use a temporary cache root through `DOCKHAND_INDEX_CACHE`, and the 80 seconds of a warm `outdated deno` were per-candidate interpreter startups in release discovery, now one batched session (87s to 27s).

### 3. Harden the first-use path

Implemented on 2026-09-16; see the [hardening report](activity/2026-09-16-first-use-hardening.md). A wrong `--tree` fails immediately, distfile 404s name the URL and explain unreleased tags, `assess --version` echoes the resolved release, `outdated` names the catalog it consulted, index staging happens once per command, the three change commands have their own help prose and examples, and `--version` exists.

### 4. Support commit-qualified Cargo Git dependencies

Implemented on 2026-09-16; see the [Git reference report](activity/2026-09-16-cargo-git-references.md). Source identity now records each Git crate's selector (branch, tag, `rev`, or default branch) with its exact commit. The cargo PortGroup can only declare branch selectors, so the other kinds follow the port's `cargo.offline_cmd`: an offline build refuses them with an actionable error, and a port that disables offline mode leaves them to Cargo's online resolution and reports them. The maintained-override comparison is unchanged. Master had already moved past the historical Codex commit, so the live replay bumped Codex 0.154.0 to 0.155.0-alpha.15 through complete preparation with the V8 pin untouched.

### 5. Resolve the remaining demonstrated platform-coverage gaps

Implemented on 2026-09-16 by classification rather than a new dimension; see the [classification report](activity/2026-09-16-unmodeled-read-classification.md). The five controls read the deployment target or minor version only where it cannot select a source declaration: as text in build-phase commands (abendrot, bun, warzone2100) or as a condition guarding configure-only branches (fldigi). The scanner now refuses such reads only where they can reach a version, distfile, or checksum declaration, and the four ports assess as input-found; abendrot, warzone2100, and fldigi pass candidate checks. mrustc's remaining inputs are compiler-selection and compiler-flag probes of the installed toolchain, which the refusal now names by Portfile line; it stays refused by design.

### 6. Broaden PR observation

Implemented on 2026-09-17; see the [PR observation report](activity/2026-09-17-pr-observation.md). `refresh` now records, for an open PR, the head's draft state, mergeability with the forge's detail, the latest review from each reviewer, and check runs and commit statuses with failing names; `status` shows the line. Publication still completes when the PR is opened or updated. Observation authorizes nothing: no rerun, comment, push, or edit follows from it.

## Later capabilities

### Dependent verification follow-up

Direct-dependent discovery, isolated Tart builds, per-target images, and full-cohort publication checks are implemented. Remaining work includes baseline comparison when an unrelated dependency causes a failure, a separate design for automatic downstream revision edits, and GitHub dependent coverage. Keep those separate from shared-release subports.

### Broader selectors and multi-target intake

Names and indexed subports work now; `assess` and `outdated` also support maintainer/category selection, and `assess` can scan the tree. Batch preparation remains future work: expose one result per requested target and preserve independent failures, admission rules, and attachment behavior. Do not add a generic batch interface merely to implement one shared release.

### GitHub verification refinements

Consider controlled reruns, safe updates of already-pushed verification branches, and broader workflow coverage. After a local branch rename, forge verification now pushes to the PR's recorded head branch while the local name only locates the commit; see the [remote branch report](activity/2026-09-17-remote-branch-identity.md). Shared-run cancellation, missing-run diagnostics, delayed-run recovery, offline cancellation, and log-cache retention already exist; see the [GitHub exercise](activity/2026-09-15-xplr-github-exercise.md).

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
- **Exported surface and documentation:** the whole-tree export audit was done on 2026-09-17; see the [audit review](reviews/2026-09-17-exported-surface-audit.md). Repeat `make deadcode` when a milestone lands, and keep exports that exist only for tests documented as such. Package overviews exist; improve operation/recovery contracts when useful. Keep implementation history in activity reports and current ownership in `components.md`; review raw benchmark retention without automatically deleting history.
- **Caches and transport:** preparation precedes Tart capacity reservation; exact candidate indexes, incremental index reuse, shared `fetch`, and explicit cache retention are implemented. Keep cache ownership in callers. The cold manual-branch Terraform cleanup exercise generated a full index twice, in discovery and Tart staging (about four minutes each); the [PortIndex storage design](portindex.md) now owns that consolidation as Next item 2. Reconsider splitting PortIndex query/staging only for a concrete consumer boundary.
- **Rate limits:** consider account-wide GitHub cooldown coordination only if concurrent measurements justify it beyond the existing persisted per-record deadlines.

## Completed capabilities

Extend these through their existing paths rather than treating them as new roadmap items:

| Capability | Established behavior / evidence |
| --- | --- |
| Target-based contribution workflow | Exact-source port/subport resolution; early contribution identity; transactional acceptance, replay, and frozen retries; named verify/publish/status/wait/cancel; explicit manual checkout verification. [Integrated validation](activity/2026-09-16-target-workflow-validation.md). Local abandonment and explicit PR refresh retire settled contributions while preserving corrections and history. [Lifecycle validation](activity/2026-09-16-contribution-lifecycle.md). |
| Release discovery and assessment | GitHub/GitLab catalogs and supported native HTTP regex livechecks shared by bump/outdated; local assess with optional candidate probes; maintainer/category selectors and whole-tree assessment. Terraform selected 1.16.3 and passed Tart verification/publication preview. |
| Source preparation | Evaluator-guided calculated versions, revision bumps, checksum refresh, scoped series updates, conditional/multiple archives, preserved pins, and supported Go/Cargo regeneration. Source-bound platform operands and manifest/auxiliary archive separation are implemented; see [coverage validation](activity/2026-09-16-bump-coverage-validation.md). Rejection-only fetch guards and local planning before helper/download work are implemented; Wasmer 7.4.2 passed preparation and Tart verification. Declared patch files are checked against the new source before any build; see the [patch check report](activity/2026-09-17-patch-check.md). |
| Shared-release contributions | Explicit `--shared-release` authorization; source/checksum-owner evidence; affected/protected members on immutable revisions; isolated sibling verification and complete publication coverage. Metadata-only parents remain in the change. Initial multi-target provider support is local; GitHub reports the unsupported coverage. [Scope contract](activity/2026-09-16-shared-release-scope.md). |
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

The [structural review](reviews/2026-09-16-bump-machinery-structure.md) of the bump-to-PR machinery is implemented: embedded leases and typed step outcomes, shared lock and atomic-replacement helpers, the `portedit` workspace and archive store, checksum refresh through the observed archive plan, the `workflow/policy` leaf, one `subprocess` runner, the version-input set, the `macports/fidelity` package, and counted waits; see the [completion report](activity/2026-09-16-structural-completion.md). Whether a long expected wait should settle as needs-attention remains a policy decision.

Earlier findings about whole-state Git-ledger writes, SQLite migration structure, repeated phase inference, state/workflow ownership, CI, workflow organization, shared Tart mechanics, and the unused placeholder planner have been addressed. Remaining useful review findings are represented above. Do not reintroduce the discarded Git ledger or require v1 feature parity as a prerequisite for this queue.

End meaningful milestones with targeted user-path and recovery exercises. For coverage changes, replay the pinned 147-Portfile corpus and the targeted controls, distinguishing input discovery, candidate checks, actual archive preparation, and builds. Preserve Terraform/Helm/gh/Deno and the new Wasmer path as controls. Repeat a provisioning matrix or create live PRs only when the changed behavior warrants it. Historical exercises and reviews are evidence, not additional queues.
