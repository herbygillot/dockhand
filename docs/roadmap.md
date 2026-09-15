# Dockhand roadmap

This document is the current source of truth for implementation priorities. The [architecture](architecture.md), [component map](components.md), [CLI design](cli-design.md), and [state design](state.md) define behavior and boundaries. Activity reports record completed work and its validation; they do not maintain the current queue.

The order within **Next** is intentional. Other sections describe accepted direction, unresolved design, or explicitly deferred scope without promising implementation order.

Last updated: 2026-09-15.

## Next

Source archive and Go/Rust dependency preparation are implemented; the next priority connects downstream discovery to verification scheduling. Small independent items may land separately; do not combine them into a single architectural rewrite.

Keep live exercise results in activity reports. Add regressions for defects that remain, rather than re-queuing cancellation, recovery, or provisioning work that has already passed its exercise.

### 1. Plan and execute dependent verification

Add downstream coverage without turning workflow into a generic graph engine.

- `macports` discovers reverse dependents and the dependency closure for each selected target against the frozen source tree. PortIndex lookup, direct reverse indexing, and transitive closure queries are implemented. `macports/dependents` now stages that index against frozen source and evaluates a deterministic coverage cohort, retaining per-target failures and index gaps; workflow still needs to invoke discovery for the prepared source outside a write transaction and persist the result.
- `macports/portedit` proposes any required revision edits separately from the verification coverage plan.
- `verify` records the concrete target/configuration questions that need answers.
- `workflow` schedules an isolated attempt for each target/configuration and lets provider capacity determine parallel or sequential execution. The multi-attempt scheduler and aggregate outcomes are implemented; the source-bound discovery result still needs to populate these plans with per-target environment requirements.
- Each attempt owns its VM and artifacts. Conflicting dependents therefore do not need to coexist in one guest.
- Results distinguish a failure in the selected downstream port from a failure caused by another dependency in its resolved build closure.

The first implementation should favor explicit per-target results and conservative coverage. Artifact reuse, baseline comparison, and more aggressive scheduling can follow after the basic model is measured.

## Planned

These items have a useful place in the current architecture but are not the immediate implementation queue.

### Standalone checksum refresh

Implement `refresh-checksums` through the existing preparation, download, evaluation, branch integration, verification, and publication path. Share checksum mechanics with version bumps rather than creating a second workflow.

### Broader selectors and multi-target intake

Extend the current single-port selector deliberately. A multi-target request should expose one result per target, preserve independent failures, and use the same admission and attachment rules as current jobs.

### Upstream discovery

Expose the existing discovery and version-assessment boundary through `outdated`. Unknown or incomplete observations remain visible. Discovery does not create branches, jobs, or publication authority.

### Pull-request observation

Teach resident driver cycles to refresh PR head, mergeability, review, CI, and conflict observations. Publication still completes when the PR is opened or updated; later observations remain attached to the contribution.

### MacPorts Base compatibility and fetch semantics

A read-only source review found no incompatible evaluator interfaces in Base 2.12.2–2.12.6, or in the representative older releases 2.11.6, 2.10.7, 2.9.3, and 2.8.1. This is source-review evidence, not runtime certification or a guarantee that current PortGroups support those older releases.

- Report the installed Base version in evaluator diagnostics and track which versions have source-review evidence versus runtime validation. An untested version should not silently appear certified, nor should version alone replace capability checks.
- Check the evaluator's required capabilities and assumptions at startup or first use. Missing metadata access should produce a clear compatibility error; unrecognized fetch internals should disable automatic source preparation with an actionable explanation.
- Validate the shared evaluator against representative Base versions on compatible hosts, covering source/resource binding, subports and variants, optional metadata, and fetch-hook inspection. Track PortGroup compatibility separately: Go hook recognition can change with the ports tree independently of Base. In particular, test the assumptions about target record keys, `user${hook}` procedure names, and the `global {*}[info globals]` body prefix.
- Keep source binding, metadata access, and fetch-hook inspection identifiable within the MacPorts adapter. Retain one shared Tcl implementation while the contracts agree; introduce version-specific overrides when a demonstrated incompatibility requires them. V1's version selector currently has only one actual shim, `2.12.6.tcl`, so its presence alone is not evidence of historical coverage.

### Engineering follow-up

- Reduce repeated whole-tree indexing for small standalone edits, and consider staging indexes before occupying VM capacity. The concurrent exercise left a ready guest waiting on host indexing and its shared cache lock.
- Add focused tests for Tcl shell and RPC behavior, especially process exit, malformed replies, cancellation, and error propagation.
- Measure the current CLI suite, then move duplicated lifecycle/composition scenarios to app or integration tests where useful. Keep focused CLI coverage for parsing, rendering, exit codes, and representative end-to-end wiring; retain existing recovery assertions. Do not impose the old review's timing target without current measurements.
- Add a lightweight automated check of the dependency rules in `components.md`; enforce meaningful package boundaries rather than a broad stylistic lint regime.
- Audit unused exported Tcl/upstream APIs and reserved scaffolding against current callers and protocol use. Unexport, remove, or test deliberately; do not delete functioning planning code based on an older review's inventory.
- Continue the coherent-comment pass, prioritizing package responsibilities and recovery contracts over comment-count targets.
- Keep `components.md` focused on the current map, responsibilities, and dependency rules. Link to activity reports for implementation history instead of repeating it. Review how raw benchmark data is retained while preserving reproducible commands and useful conclusions; no automatic deletion of history is implied.
- Record the existing `status` contract explicitly in the principles: it reads durable observations; driver cycles perform reconciliation and external refreshes. Distinguish snapshot time from observation time.
- Revisit a shared download package only when common policy and lifecycle emerge across current callers.

## Needs design

These items should not be implemented from their existing command placeholders alone. Settle human corrections and publication behavior before expanding post-publication commands; settle review authority and requester provenance before unattended discovery can originate publishable work.

### Review controls

The design names `review accept` and `review dismiss`, but it does not yet define what creates a pending review, what acceptance authorizes, or what dismissal closes. A manual `bump` or `publish` already records direct user intent. Review controls should be designed with discovery, unattended work, and corrective edits so their durable consequences are unambiguous and revision-bound.

At minimum, the design must settle:

- whether review applies to a discovered candidate, a prepared revision, or publication authority;
- how a user inspects the exact diff and evidence before deciding;
- how acceptance is bound to the current revision and rejected after it changes;
- whether dismissal closes one revision, the whole contribution, or only pending jobs; and
- how the driver reports and applies the decision idempotently.

### Human corrections and post-publication work

Define the user-facing relationship among ordinary Git edits and the proposed `amend`, `rebase`, `verify`, and `publish` commands. The flow must preserve the stable contribution identity, create a new immutable revision, invalidate evidence that no longer applies, and update the existing PR only after applicable verification.

The design should also cover branch reassociation after a user renames a branch and MacPorts' preference for corrective changes squashed into the original contribution commit.

### Standalone publication with missing verification

Decide whether `publish` should schedule missing verification, join an applicable active attempt, or continue requiring a separate explicit `verify`. Any automatic continuation must preserve the publication authority expressed by the original command.

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

## Review triage

This ordering incorporates the findings that remain useful from Claude's four project reviews and workflow review. Earlier findings about whole-state Git-ledger writes, the SQLite migration ladder, repeated phase inference, state/workflow policy ownership, missing CI, workflow file organization, shared Tart mechanics, PortIndex placement, and the unused placeholder planner have already been addressed; they are not new pending work.

The remaining test-placement, mechanism-documentation, exported-surface, dependency-checking, and status-contract suggestions are represented above. Cross-repository evidence reuse, requester provenance, review controls, and PR observation retain their existing design/planning slots. Do not split workflow merely because it is large, reintroduce the discarded Git ledger, rename the user-selected environment variables, or require v1 feature parity as a prerequisite for this queue.

## GitHub provider follow-ups

The [xplr exercise](activity/2026-09-15-xplr-github-exercise.md) completed fork verification through PR, driver recovery, shared-run tracking cancellation, and a user-triggered rerun.

- Improve resolution of accepted pushes whose Actions run never appears, without treating delayed events as conclusively absent.
- Consider controlled rerun support, safe updates to previously pushed branches, and broader cohort/workflow coverage after the initial committed single-port path.
- Add managed retention for GitHub job-log caches; currently they are separate from VM resource pruning.
