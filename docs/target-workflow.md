# One target through the contribution workflow

Status: stages 0–5 implemented, 2026-09-16; see the [integrated validation report](activity/2026-09-16-target-workflow-validation.md). Stage 6 describes the subsequent broader-coverage roadmap; it is not part of the completed ordinary-workflow milestone. This milestone supersedes the earlier name-resolution-only prerequisite in [bump coverage](bump-coverage.md). The [roadmap](roadmap.md) remains the queue; this document specifies the work and its acceptance criteria.

## Problem and intended experience

The original Terraform exercise exposed three independent gaps: selectors matched directories rather than all port names; contributions were recorded only after successful branch integration; and automatic release discovery required a recognized GitHub/GitLab source convention. The implementation below addresses those gaps together.

The saved bump `job_PRB7HKJZ6U2NNTSNFI7LTMRUXD` failed during automatic discovery, before branch creation. The passing `job_T55QF767QL6HGQHXRHMZKTDAAY` was a separate verification of unmodified `master` at `87ff2b89b11d1666d39a51929499c5787bebecdf`, where terraform-1.16 was 1.16.0. It was not a successful version update. Earlier explicit-version archive preparation exercises establish editability, not automatic discovery or this complete user workflow.

The intended ordinary sequence is:

```sh
dockhand bump terraform-1.16
dockhand status terraform-1.16
dockhand verify terraform-1.16
dockhand publish terraform-1.16
```

Bump still verifies by default. Separate verification is useful after a failure or corrective edit; it is not a mandatory extra build. `bump <target> --publish` retains its combined meaning. An optional explicit version remains supported. No public `--subport` is needed, and branch names and job IDs are optional selectors for exceptional cases, not required handoffs.

## Behavioral decisions

- A target is a convenient selector, not the durable identity. A repository-scoped `ChangeID` identifies the contribution across jobs, revisions, branch renames, and PR updates. Reuse `record.Change`; do not add a parallel work-item model.
- A contribution exists once preparation is durably accepted. It may have no branch or revision yet. A planned branch name must not be displayed as an existing branch.
- Keep the initiating target distinct from the full set of changed targets and from verification-only dependents. Start with one initiating target. Variants and build settings are evidence inputs, not implicit new contribution identities.
- `verify <target>` and `publish <target>` select a unique open contribution in the selected repository. A missing, failed-before-preparation, renamed, or deleted branch produces an actionable result; it must not trigger fallback to the current checkout or to an older successful job.
- Zero matches means no contribution to continue; multiple matches means ambiguity. List the branch or preparation state and contribution ID. Preserve `--branch` for explicit branch selection and provide `--change` for ambiguity involving contributions that have no branch. If a target is also supplied, it must agree with the selected contribution.
- New preparations resolve targets in their captured upstream source. Continuing commands first select the recorded contribution, then capture and validate its branch. They must not resolve against a fresh upstream index or the unrelated current checkout and accidentally select a different source.
- For manual verification, make checkout capture explicit with `verify <target> --working-tree`; retain committed `--branch` verification and branch-based target inference. If the checkout is the selected contribution branch, tracked edits/staged additions can be recorded under that contribution through existing capture/adoption rules. An unrelated checkout remains an explicit standalone verification. Never silently include untracked files. GitHub verification still requires committed contents.
- Bare target verification uses committed contribution contents. If that branch is checked out with uncommitted changes, report that distinction and direct the user to working-tree verification or amendment rather than quietly verifying an older commit. Check branch occupancy across worktrees through existing Git helpers.
- Publication uses the selected contribution's current committed contents and applicable passing evidence. A changed tree needs verification; an identical tree may reuse evidence under the existing build-input and coverage rules. Standalone `publish` does not implicitly build, commit edits, or grant new publication authority.
- A repeated equivalent bump joins pending work or reports the existing result. A retry after failure creates a new job under the same contribution; terminal history is preserved. Once a release has been resolved, retry that exact release instead of changing it under the user's feet. A different explicit version, or a newer discovered release on a later bump, must not overwrite an existing branch or human corrections; initially report the existing contribution and require it to be resolved or explicitly selected for a supported correction. Automatic retargeting is outside the first slice.
- A successful already-current discovery creates no branch and starts no build. Finish the initial contribution intent without leaving an empty open contribution that blocks later bumps. Add a small explicit no-update outcome/disposition if needed; do not label it merged, published, or failed.

## Delivery order

### 0. Make present outcomes understandable

Before changing selection semantics, improve the existing status/result projection. Show action, evaluated version, source kind and branch, whether an update branch actually exists, and what remains before publication. Failed preparation, already-current discovery, standalone verification, prepared-but-unverified, verified, and published are different outcomes.

Use structured state to render next actions. Do not parse error strings to decide what happened or suggest target-based commands before they exist. Preserve job/attempt detail and JSON diagnostics. A read-only status command must remain a database snapshot, not fetch Git refs, inspect a VM, or reconcile the workflow. Label readiness as recorded when external state has not been refreshed.

Acceptance: reproduce the two Terraform records as fixtures and ensure the passing standalone build is never described as a bumped or publishable update. Existing branch-ready results display their branch even after verification fails or evidence is reused.

### 1. Unify port and subport resolution

Build one source-bound resolution path for bump, assess, outdated, and explicit verification. Use the captured source's PortIndex to locate the exact port name, then evaluate the owning Portfile for that name. Keep explicit directory/Portfile paths where useful and retain internal subport identity. Do not infer ownership by stripping version suffixes or substitute a parent port when an index lacks coverage.

A small `internal/macports/selection` package is justified here: it composes indexed lookup and native evaluation for several callers. `portindex` retains index staging, cache validity, lookup, and coverage reporting; `eval` retains Tcl execution. Inject the source-bound index access needed by selection, rather than teaching the native evaluator to fetch indexes or access Git/state. Reuse the existing snapshot and staging lifetimes. Migrate grouped survey and explicit intake through the shared mapping/validation, without moving discovery or workflow policy into selection.

Only remove `--subport` once all relevant callers use this path. Update help, examples, and completion together. Measure cold and warm lookup cost, and preserve source/PortGroup/platform-sensitive cache validation. New or changed ports in working-tree snapshots must be indexed against that snapshot.

Acceptance: terraform and terraform-1.16 are distinct; generated Python subports resolve correctly; stale/incomplete indexes do not misselect; selected target names agree across commands. Use explicit-version Terraform preparation as the control so a discovery failure cannot mask a resolution defect.

### 2. Establish contribution identity before preparation

Change acceptance and integration together. In one short state transaction, select or create the contribution and record the accepted job. Store an indexed initiating-target selector scoped to the repository, separate from the contribution's complete changed-target scope. Allow branch-less/revision-less contributions with explicit validation. Jobs and immutable source snapshots remain the execution history; do not build an event store or generic scheduler.

Keep request-ID idempotency separate from user retries: replaying one request yields its original receipt; a new retry is a new request/job attached to the existing contribution. Concurrent equivalent bump requests must not create two contributions or two active preparations. Use transactional selection/revalidation and existing claims for driver ownership; neither a global lockfile nor network work inside the transaction is needed. Do not enforce one open contribution per target globally: manually adopted branches and existing duplicate contributions must remain representable, with ambiguity surfaced.

Integration fills the existing contribution's branch and first revision. Preserve candidate checkpoints and conditional ref updates. `GeneratedCommit` is currently immutable from insertion; introduce a narrowly validated unset-to-set transition at first confirmed generated integration, after which it remains immutable. Do not loosen provenance rules just to accommodate early creation. Include interrupted Git integration, stale claim completion, missing objects, and cancellation in this change.

Migration must preserve existing changes, revisions, PR associations, and evidence. Backfill the initiating target for unambiguous single-target changes. Associate legacy preparation jobs only where persisted relationships prove ownership; never combine unrelated jobs merely because their target matches. Legacy standalone verification remains standalone. Cover existing ambiguous/multi-target records conservatively and preserve a database upgrade path.

Acceptance: failure before release resolution remains selectable; retry keeps contribution identity; concurrent submissions and uncertain transaction acknowledgments do not duplicate work; branch integration after a crash is recoverable; already-current/no-update and abandoned work do not obstruct a later contribution.

### 3. Connect commands through the contribution

Add one workflow selection/binding policy used by verify, publish, and target-oriented status; extend wait/cancel with the same semantics where applicable. Keep Cobra responsible for syntax and presentation. Make target-oriented status the normal view while retaining explicit job inspection (for example `status --job ID`) rather than guessing whether a positional token is a job or a port name.

Selection returns a concrete contribution and expected revision. Capture/evaluate the chosen source outside the state transaction; revalidate selection, revision, and branch preconditions when accepting work. Never enqueue an instruction to resolve "whatever is latest" when a driver eventually picks it up. A concurrent new revision cannot redirect an already accepted job. Freeze the affected job set for wait/cancel; later jobs do not silently join it.

Wire preparation retries through the same contribution. For a contribution with no prepared branch, verify explains that preparation must succeed first; it does not convert the original checkout into the requested update. A verification failure leaves a prepared branch available for a retry. Multiple contributions remain an explicit choice, not "latest passing wins."

Human amendments/rebases must still go through the existing scope, revision-adoption, and evidence checks. `--working-tree` makes manual capture explicit without adding an automatic commit step. `publish <target>` checks current committed source and the full required evidence; dirty working-tree-only evidence cannot authorize publishing a different committed tree. Preserve an existing PR's head branch when local and published branch names differ.

Acceptance: an explicit-version bump can be followed by verify and publish using only its target; failed preparation cannot fall through to master; failed verification can be retried; source changes invalidate evidence as appropriate; two repositories and two independent contributions never cross-select. Include Tart and GitHub binding regressions, current-branch edits, detached HEAD, missing/renamed branches, and changes during admission.

### 4. Discover eligible releases beyond forge catalogs

Separate interpreted release-discovery facts from source-editing facts. Terraform's Portfile already declares an evaluated `livecheck.type regex`, a release-listing URL, and a regex scoped to its selected release series. It should not need a GitHub PortGroup to discover candidates.

Extend `macports/source` to describe supported discovery sources independently of its forge interpretation. Keep a small candidate-observation boundary in `upstream`: existing GitHub/GitLab catalogs and an HTTP regex listing are concrete implementations. Generic HTTP discovery is not a forge repository and must not be squeezed into `forge.Repository`. Use the shared `fetch` transport; keep Tcl regex extraction and MacPorts `vercmp` in `macports/eval`, extending the existing version-selection adapter. Do not translate Tcl expressions into Go regexes or scrape human-readable `port livecheck` output.

The first supported non-forge case is an evaluated HTTP(S) regex listing whose captures identify versions. Honor required livecheck request options, or explicitly report unsupported options/custom hooks; do not silently ignore behavior that changes the response. Collect all matches using native semantics, deduplicate repeated links to the same version, apply the established stable-version policy, and compare with MacPorts semantics. Empty, malformed, incomplete, or unavailable observations are unknown/error, never already-current. Preserve source URL, selected version, observation time, and available response identity as compact evidence; no large page BLOB is required.

For terraform-1.16, the evaluated livecheck filter supplies the 1.16.x boundary. Do not infer series by splitting the port name, choose a newer different series, or change independently pinned siblings. Keep GitHub tags as the default catalog and respect `github.tarball_from` release preference. Do not silently switch to another source after an authentication or network failure.

Feed discovered versions into the same evaluator-driven candidate checks and archive preparation used by explicit versions. No per-port edit scheme or Terraform-specific discovery code is needed. Extend persisted release/check validation to support discovered archive versions: today's archive release validation assumes a nonempty explicitly requested version. Record whether the choice was explicit or discovered and freeze a selected release for retries. A changed listing must not silently retarget an accepted preparation. Downloaded archive checksums and existing artifact checks remain separate from release-listing evidence.

Use this service for both bump and outdated. Keep assess local: an explicit-version assessment probes editability, not remote release availability. Report discovery support separately from editing support so a successful assess is not advertised as proof of an automatic bump.

Acceptance: unordered links, numeric ordering, duplicate links, prereleases, other series, current/ahead versions, malformed responses, Tcl-specific patterns, and failed requests. An automatic Terraform bump selects the newest eligible 1.16.x candidate from a controlled listing, and its edit matches an explicit bump to that version. Then exercise the current real listing and report the observed version/time rather than fixing "latest" in a test.

### 5. Complete the preparation reliability slice

Return to stage 1 of [bump coverage](bump-coverage.md): distinguish rejection-only fetch guards, preserve unsupported-platform restrictions, and plan all required contexts before dependency archive transfers/helper execution. Wasmer remains the concrete acceptance case. Use the new contribution lifecycle and truthful outcome rendering to carry any remaining preparation/build failure without losing the selected update.

This work does not depend on shared-subport releases. Keep it a separate change from target selection and discovery so a passing Terraform path does not imply Wasmer coverage. Run explicit and automatic Terraform controls, gh/Deno/Helm archive controls, and a supported-platform Wasmer preparation/verification exercise. Publication regression tests are required; a live PR is a final exercise when there is a suitable actual update and the user has authorized it.

### 6. Broaden scope after the ordinary workflow holds

The remaining bump-coverage capabilities are platform observations, complete shared-release subport scope, and manifest-bearing archives alongside pinned auxiliary files. Their implementation order, and contribution lifecycle/cleanup work now preceding them, are maintained in the [roadmap](roadmap.md). Downstream dependency verification is already a separate coverage plan: preserve isolated builds and require every required result before publication. Do not confuse dependents with targets whose source declarations changed.

Shared releases add changed targets/revisions under the same contribution; they do not require exposing child jobs as the user's primary handle. Initially only the initiating target selects the contribution by name. If related targets later become aliases, add them explicitly with ambiguity rules; do not treat every dependent as a contribution alias. Baseline comparison for unrelated downstream failures and automatic dependent revision bumps remain separate roadmap work.

## Package boundaries and implementation discipline

| Concern | Owner |
| --- | --- |
| CLI selectors, source-choice flags, next-step output | `cli` |
| Composition and runtime/index dependencies | `app` |
| Name-to-Portfile resolution in a bound snapshot | `macports/selection`, backed by `portindex` and `eval` |
| Contribution lookup, acceptance, retries, source binding, readiness projection | Focused files in `workflow`; existing shared engine |
| Persistent identity, initiating target, revision/job relationships | `record`, `state`, `state/sqlite` |
| Interpreted livecheck/source conventions | `macports/source` |
| Candidate collection and release selection | `upstream`, using forge adapters or HTTP observations |
| Native Tcl regex/version semantics | `macports/eval` |
| Edit planning, archive ownership, dependency regeneration | Existing `macports/portedit`, `distfiles`, `dependency` boundaries |
| Snapshot/ref operations, evidence applicability, publication | Existing `git/changeset`, `verify`, and `publish` contracts |

One focused selection package is enough initially. Do not create a second workflow/driver, a generic pipeline/DAG, or a provider registry just for two discovery paths. No additional external Go library is required for this plan. If candidate adapters need separate files, add them inside upstream before introducing more packages.

Deliver stages as independently reviewable commits; stage 2's state and integration changes must land together with their tests. Stage 4 can be developed independently once target-resolution inputs are established, but the user-path milestone requires stage 3. Update CLI/state/component documentation with implemented behavior as each stage lands, leaving proposals explicitly marked until then.

Use behavioral fixtures for the reported failures, then an explicit-version Terraform path, then the automatic-version path, then Wasmer. Add concurrency/recovery tests for changed state boundaries and retain existing evidence/PR safeguards. Run focused checks while developing, then the full Go suite, race suite, vet, and build at the integrated milestone. Measure name lookup and avoid repeating full-tree indexing per command. No live DB record repairs, merges of user contributions, port-tree changes, or PR creation are part of this planning pass.
