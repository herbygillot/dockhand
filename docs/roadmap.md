# Dockhand roadmap

This document is the current source of truth for implementation priorities. The [architecture](architecture.md), [component map](components.md), [CLI design](cli-design.md), and [state design](state.md) define behavior and boundaries. Activity reports retain implementation history and validation; they are not additional queues.

Last reconciled: 2026-09-17, after the whole-tree coverage survey and the python work that followed it. The survey assessed 41,716 index rows and found 39.3% input-found; its data stays outside the checkout, and the activity reports quote what matters. The six items of the previous queue are implemented and listed under completed capabilities. **Next** is ordered. Later capabilities and maintenance work are not prerequisites unless stated explicitly.

## Next

### 1. Trace checksum values held in data tables

517 qt5 and qt6 subports, and 147 entries elsewhere, are refused with "checksum value has no unique literal owner": their `checksums` command reads `rmd160`, `sha256`, and `size` out of a `set`-built table with `lindex`, so the literal that must change lives in the table, not in the declaration. Ownership through one level of literal table indexing is a bounded extension of `macports/distfiles`, which already binds declarations to exact spans; keep the fidelity rule that the candidate must change only the owned literals. Do not special-case qt. This is the qt family's real boundary, not the deployment target the survey had in front of it.

### 2. Recognize versions carried as `*.setup` arguments

17,040 survey entries, 41% of the index, have no editable version input because the perl5, R, and ruby PortGroups carry the version inside `perl5.setup`, `R.setup`, or `ruby.setup`; recognizing that argument would move about 16,500 of them. It ranks after the item above because those families move less than python: 780 perl bumps a year against 2,440 python ones, and ruby and R far fewer. The version-input probe already handles command-substitution arguments, so this is an input-recognition extension with the same fidelity checks, followed by the corpus replay.

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

Legacy digests are handled: a group with md5 or sha1, or without sha256, is rewritten with correct source ownership when its archive is refreshed, and `--keep-old-checksums` keeps it as written. What the survey leaves after the queue above: about 110 ports whose only master sites are ftp, which the direct downloader does not speak; 1,061 Portfiles that read the host filesystem, enumerate directories, or run a process during evaluation, so the captured-tree model cannot vouch for their probe; and 2,256 python stubs, which are not distfile ports and now bump through their newest subport. Arbitrary livecheck scripts, multiple independent version inputs, new release-series creation, and coordinated Rust/bootstrap maintenance still require separate designs rather than per-port special cases.

### Shared-release coverage on GitHub

Local verification of a shared release builds the newest subport by default and every subport with `--all-subports`; the GitHub provider still reports multi-target coverage as unsupported and the pull request workflow builds the siblings. Extend the GitHub provider when a shared release must be proven there before publication.

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
- **Exported surface and documentation:** the whole-tree export audit was done on 2026-09-17; see the [audit review](reviews/2026-09-17-exported-surface-audit.md). `make deadcode` was clean after the 2026-09-17 coverage work. Repeat it when a milestone lands, and keep exports that exist only for tests documented as such. Package overviews exist; improve operation/recovery contracts when useful. Keep implementation history in activity reports and current ownership in `components.md`; review raw benchmark retention without automatically deleting history.
- **Caches and transport:** preparation precedes Tart capacity reservation; exact candidate indexes, incremental index reuse, shared `fetch`, and explicit cache retention are implemented. Keep cache ownership in callers. The cold manual-branch Terraform cleanup exercise generated a full index twice, in discovery and Tart staging (about four minutes each); the [PortIndex storage design](portindex.md) now owns that consolidation as Next item 2. Reconsider splitting PortIndex query/staging only for a concrete consumer boundary.
- **Rate limits:** consider account-wide GitHub cooldown coordination only if concurrent measurements justify it beyond the existing persisted per-record deadlines.
- **Test and evaluation throughput:** the cost unit is the MacPorts interpreter. On 2026-09-17 the uncached suite took 232s of wall time on 18 cores with package times summing to 983s; one archive preparation with modeled contexts started 34 interpreters, and no test ran in parallel. Three levers were pulled that day: independent tests are marked parallel, with the tests that set environment or change directory kept serial; every multi-context observation writes the candidate once and observes its profiles concurrently, which speeds real bumps and assessments as well; and `assess` works through its ports in a bounded pool, which took the 147-port corpus from 217s to 28s; see the [throughput report](activity/2026-09-17-test-throughput.md) and the [corpus replay](activity/2026-09-17-corpus-replay.md). Still open, in this order: reuse one native interpreter session across resolve, baseline, candidate, and final evaluations (the session type exists and the batched version probe already uses it; this falls out of the session-and-plan refactor below); and test operand and regeneration behavior at their unit seams with one or two end-to-end cases each instead of a complete preparation per case. Keep per-test timing visible (`go test -json` aggregated, or gotestsum) so the number does not creep back; `testing/synctest` is available for the engine's time-driven tests.

## Completed capabilities

Extend these through their existing paths rather than treating them as new roadmap items:

| Capability | Established behavior / evidence |
| --- | --- |
| Target-based contribution workflow | Exact-source port/subport resolution; early contribution identity; transactional acceptance, replay, and frozen retries; named verify/publish/status/wait/cancel; explicit manual checkout verification. [Integrated validation](activity/2026-09-16-target-workflow-validation.md). Local abandonment and explicit PR refresh retire settled contributions while preserving corrections and history. [Lifecycle validation](activity/2026-09-16-contribution-lifecycle.md). The processing cycle observes open PRs and retires merged ones, `status` shows one row per port, a branchless needs-attention closes its empty contribution, and the ports tree can never be the dockhand checkout; see [cycle observation](activity/2026-09-17-cycle-observes-prs.md), [rows per port](activity/2026-09-17-rows-per-port.md), and the [ports-tree guard](activity/2026-09-17-ports-tree-guard.md). |
| Command interface | One JSON envelope per command, an info-level minimum for every result, macOS release names for platforms, foreground defaults with `--no-publish` and `--detach`, and a live `status` table that is also the processor unless `--print`; see the [envelope](activity/2026-09-17-json-envelope.md), [info minimum](activity/2026-09-17-info-minimum.md), [foreground defaults](activity/2026-09-17-foreground-defaults.md), and [status processes](activity/2026-09-17-status-processes.md) reports. |
| Python and stub bumps | A stub port bumps through its newest versioned subport as one shared release, with the stub's name on the contribution; the newest subport is verified locally by default and `--all-subports` builds them all; livechecks resolve through MacPorts' own checker files, so pypi discovery is read, not restated; an obsolete port follows its replacement. PR 34737 (`py-idna` 3.20) was published this way. See [stub bumps](activity/2026-09-17-stub-bumps.md), [livechecks through MacPorts](activity/2026-09-17-livecheck-through-macports.md), and [obsolete followers](activity/2026-09-17-obsolete-followers.md). |
| Coverage corpus | The pinned 147-Portfile corpus was replayed on 2026-09-17 after that day's coverage changes: 119 input-found, 18 unsupported, 10 unknown, against 85/54/8 at the 2026-09-15 baseline; the named controls pass candidate checks; the five ports that load the qt4 PortGroup are unknown for its read of the installed Qt, a deliberate classification. See the [replay report](activity/2026-09-17-corpus-replay.md) and its [table](activity/2026-09-17-corpus-replay.tsv). Replay again after coverage changes; the pooled `assess` makes it a half-minute run. |
| Owned publication, PortIndex reuse, first use, Cargo Git references, unmodeled reads, PR observation | The six items of the 2026-09-16 queue: publication refuses pushes to repositories the login does not own ([report](activity/2026-09-16-publication-owned-head.md)); one shared PortIndex cache with incremental generations ([report](activity/2026-09-16-portindex-consolidation.md), [follow-ups](activity/2026-09-16-portindex-follow-ups.md)); hardened first-use errors and help ([report](activity/2026-09-16-first-use-hardening.md)); commit-qualified Cargo Git dependencies ([report](activity/2026-09-16-cargo-git-references.md)); reads of unmodeled dimensions classified by where they can reach a source declaration, now including the deployment-target setter ([classification](activity/2026-09-16-unmodeled-read-classification.md), [sink](activity/2026-09-17-deployment-target-sink.md)); and PR observation of draft state, mergeability, reviews, and checks ([report](activity/2026-09-17-pr-observation.md)). |
| Release discovery and assessment | GitHub/GitLab catalogs and supported native HTTP regex livechecks shared by bump/outdated; local assess with optional candidate probes; maintainer/category selectors and whole-tree assessment. Terraform selected 1.16.3 and passed Tart verification/publication preview. |
| Source preparation | Evaluator-guided calculated versions, revision bumps, checksum refresh, scoped series updates, conditional/multiple archives, preserved pins, and supported Go/Cargo regeneration. Source-bound platform operands and manifest/auxiliary archive separation are implemented; see [coverage validation](activity/2026-09-16-bump-coverage-validation.md). Rejection-only fetch guards and local planning before helper/download work are implemented; Wasmer 7.4.2 passed preparation and Tart verification. Declared patch files are checked against the new source before any build; see the [patch check report](activity/2026-09-17-patch-check.md). Legacy checksum groups are modernized when their archive is refreshed, or kept as written with `--keep-old-checksums`; 1,486 of the 1,554 survey entries refused for their block are input-found since; see [modernization](activity/2026-09-17-checksum-modernization.md) and [the flag](activity/2026-09-17-keep-old-checksums.md). |
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

The [resolution consolidation](reviews/2026-09-17-version-resolution-consolidation.md) gathered version spellings, the selection core, source interpretation, and the release selection into single homes; its fifth move, observation-based read classification, is recorded there as deferred with the reason. The [structural review](reviews/2026-09-16-bump-machinery-structure.md) of the bump-to-PR machinery is implemented: embedded leases and typed step outcomes, shared lock and atomic-replacement helpers, the `portedit` workspace and archive store, checksum refresh through the observed archive plan, the `workflow/policy` leaf, one `subprocess` runner, the version-input set, the `macports/fidelity` package, and counted waits; see the [completion report](activity/2026-09-16-structural-completion.md). Whether a long expected wait should settle as needs-attention remains a policy decision.

Earlier findings about whole-state Git-ledger writes, SQLite migration structure, repeated phase inference, state/workflow ownership, CI, workflow organization, shared Tart mechanics, and the unused placeholder planner have been addressed. Remaining useful review findings are represented above. Do not reintroduce the discarded Git ledger or require v1 feature parity as a prerequisite for this queue.

End meaningful milestones with targeted user-path and recovery exercises. For coverage changes, replay the pinned 147-Portfile corpus and the targeted controls, distinguishing input discovery, candidate checks, actual archive preparation, and builds. Preserve Terraform/Helm/gh/Deno and the new Wasmer path as controls. Repeat a provisioning matrix or create live PRs only when the changed behavior warrants it. Historical exercises and reviews are evidence, not additional queues.
