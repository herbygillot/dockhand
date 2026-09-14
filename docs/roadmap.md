# Dockhand roadmap

This document is the current source of truth for implementation priorities. The [architecture](architecture.md), [component map](components.md), [CLI design](cli-design.md), and [state design](state.md) define behavior and boundaries. Activity reports record completed work and its validation; they do not maintain the current queue.

The order within **Next** is intentional. Other sections describe accepted direction, unresolved design, or explicitly deferred scope without promising implementation order.

Last updated: 2026-09-14.

## Next

### 1. Validate explicitly selected Tart images

Automatic image selection follows the evaluated `use_xcode` requirement, and setup writes a manifest into every image it provisions. An explicit `--image` still bypasses capability selection.

The next slice should:

- inspect the image manifest when an image digest is first encountered;
- validate the macOS release, architecture, MacPorts prefix, and developer-tools profile;
- reject an image without full Xcode when the evaluated target requires it;
- cache the observed capabilities by immutable image digest in SQLite; and
- include the capability identity in accepted build configuration and evidence-reuse comparison.

A changed image digest requires another observation. An image name alone is neither capability evidence nor verification identity.

### 2. Plan and execute dependent verification

Add downstream coverage without turning workflow into a generic graph engine.

- `macports` discovers reverse dependents and the dependency closure for each selected target against the frozen source tree.
- `prepare` proposes any required revision edits separately from the verification coverage plan.
- `verify` records the concrete target/configuration questions that need answers.
- `workflow` schedules an isolated attempt for each target/configuration and lets provider capacity determine parallel or sequential execution.
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
- Reassess the remaining workflow-only seams after the Git changeset extraction; keep state-machine phase handlers together unless another dependency boundary emerges.
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
- immutable committed and working-tree source capture with native MacPorts evaluation;
- Tart verification with shared capacity, result reuse, retained diagnostics, and garbage collection;
- base and full-Xcode Tart provisioning through `setup`, with automatic profile selection;
