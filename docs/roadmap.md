# Dockhand roadmap

This document is the current source of truth for implementation priorities. The [architecture](architecture.md), [component map](components.md), [CLI design](cli-design.md), and [state design](state.md) define behavior and boundaries. Activity reports record completed work and its validation; they do not maintain the current queue.

The order within **Next** is intentional. Other sections describe accepted direction, unresolved design, or explicitly deferred scope without promising implementation order.

Last updated: 2026-09-14.

## Next

### 1. Plan and execute dependent verification

Add downstream coverage without turning workflow into a generic graph engine.

- `macports` discovers reverse dependents and the dependency closure for each selected target against the frozen source tree. PortIndex lookup, direct reverse indexing, and transitive closure queries are implemented; workflow planning still needs to invoke them for the prepared source.
- `prepare` proposes any required revision edits separately from the verification coverage plan.
- `verify` records the concrete target/configuration questions that need answers.
- `workflow` schedules an isolated attempt for each target/configuration and lets provider capacity determine parallel or sequential execution. The multi-attempt scheduler and aggregate outcomes are implemented; coverage discovery has not yet populated these plans.
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

### Engineering follow-up

- Add focused tests for the Tcl shell and RPC boundaries.
- Continue the coherent-comment pass in packages whose contracts or recovery behavior are difficult to infer.
- Revisit a shared download package only when common policy and lifecycle emerge across the current download callers.
- Add a project license before distribution.
- Reduce duplication among current design documents without rewriting the append-only activity history.

## Needs design

These items should not be implemented from their existing command placeholders alone.

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
- Tart verification with shared capacity, result reuse, retained diagnostics, and garbage collection;
- base and full-Xcode Tart provisioning through `setup`, with automatic profile selection;
- capacity-aware validation of provisioned and custom Tart images, with immutable-digest caching and reusable environment evidence;
