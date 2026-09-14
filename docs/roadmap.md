# Dockhand roadmap

This document is the current source of truth for implementation priorities. The [architecture](architecture.md), [component map](components.md), [CLI design](cli-design.md), and [state design](state.md) define behavior and boundaries. Activity reports record completed work and its validation; they do not maintain the current queue.

The order within **Next** is intentional. Other sections describe accepted direction, unresolved design, or explicitly deferred scope without promising implementation order.

Last updated: 2026-09-14.

## Next

This order prioritizes correct contribution inputs, attribution, and recovery before expanding automatic preparation and downstream coverage. Small independent items may land separately; do not combine them into a single architectural rewrite.

### 3. Generate useful, evidence-based pull-request bodies

Use the relevant portions of `macports-ports/.github/PULL_REQUEST_TEMPLATE.md`, with v1's renderer as a behavioral reference rather than copying its assumptions. Start with `Submitted by [dockhand](https://github.com/herbygillot/dockhand)`, retain useful contribution description, and include `###### Tested on` and `###### Verification`. Show the complete body in publication previews. Keep this in the existing publication-content path, using the verification evidence selected for the exact published revision, including reused attempts.

- **Tested on:** show observed guest macOS product version (and build/architecture when available), selected Xcode or Command Line Tools version, and provider details. For Tart, identify the Tart version and image/environment, and say pristine only when that property was established for the run. Add the missing environment metadata at observation time; do not infer macOS product versions from Darwin majors, substitute the publisher host's current configuration, or invent values for older evidence.
- **Commit guidelines:** check only when all published contribution commits remain exactly as Dockhand generated them and the generation path has been checked against the guidelines. Existing subject templates have the expected port prefix, but current code does not validate the full message or arbitrary reason text. A Generated-by trailer alone is not proof. Check message formatting, body/trailer separation, and any applicable length/content rules before making the claim; otherwise leave the item unchecked or narrow its wording.
- **Squashed/minimized:** check for the single unchanged Dockhand-generated contribution commit after confirming the actual published commit range.
- **Lint:** check from a successful recorded lint step, not merely from the attempt's existence or policy.
- **Tests:** check only if a test phase actually executed successfully. `Tests=declared` alone is insufficient. If the port declared no tests or testing was skipped, explain or omit the inapplicable item rather than claiming tests ran.
- **Install:** check from a successful install step and show the command/options actually used, including `-d` and whether source-only mode was requested. Use a readable normalized command while retaining meaningful options and privilege context; do not claim the template's `-vst` invocation was used or that all dependencies were built from source when binary archives were allowed.
- **Manual checks:** retain duplicate-PR, Trac-ticket, binary-functionality, and important-variant items unchecked unless the corresponding action and its scope are actually recorded. Finding the matching PR for publication recovery is not a search for all competing changes; a default-variant build is not broad variant coverage. This rendering task does not require implementing those additional checks.

Preserve human edits to existing PR bodies when publishing again. Generate the initial body from frozen publication inputs; leave automatic refresh of previously generated sections for the post-publication design unless ownership is explicitly defined. Avoid making a generic template engine or new workflow layer. Test generated versus human/amended commits, executed versus skipped tests, reused evidence, missing environment metadata, meaningful command flags, and preservation of edited PR bodies.

### 4. Finish the outstanding exercise corrections

- Remove guest-agent release-tag assumptions from executable version checks. Use the verified installation artifact and required protocol/capabilities; keep version output diagnostic. A port executable's reported version must not be required to match its source tag.
- Make rejected saved GitHub credentials easy to identify and replace or remove. Report the credential source without exposing its value; do not silently switch identities after authentication fails.
- Show progress during initial image inspection, provider admission/source staging, and full PortIndex generation rather than leaving a stale `queued` line while work proceeds.
- Make read-only status errors on an older database schema explain the supported migration path.

Keep live exercise results in activity reports. Add regressions for defects that remain, rather than re-queuing cancellation, recovery, or provisioning work that has already passed its exercise.

### 5. Broaden source and checksum preparation

Support explicitly named checksums for a single distfile, retaining the distfile name and its Tcl expression. Then define safe handling for multiple source distfiles and their independent checksum groups. Preserve unrelated Portfile content and reject ambiguous associations.

Investigate the GitLab preparation pattern rejected by the `zix` exercise. Distinguish supported PortGroup-generated fetch behavior from custom fetch/patch logic that actually requires a dedicated preparer; do not remove the existing rejection checks wholesale. Keep host/repository/tag interpretation in `macports/source` and forge access in its adapters.

These checksum and source-editing boundaries should be shared by the dependency preparation below and by standalone checksum refresh.

### 6. Regenerate Go and Rust dependency declarations during bumps

Add explicit preparation support for `go2port`/`go.vendors` and `cargo2port`/`cargo.crates`, including `cargo.crates_github` where applicable. Derive dependency declarations and checksums from the selected new source release, using the appropriate MacPorts helpers rather than treating these declarations as ordinary single-archive checksums.

Account for added, removed, and changed dependencies, relevant module/lock files, Git-sourced dependencies, and helper failures. Keep helper execution outside database write transactions and changes in isolated preparation workspaces. Review the generated diff, reevaluate the Portfile in its frozen context, and run normal verification before publication. Preserve human-maintained options and overrides; report cases that cannot be regenerated safely.

Implement and validate Go and Rust independently through the existing preparation contract. Neither needs a separate workflow engine or a new general-purpose plugin framework.

### 7. Plan and execute dependent verification

Add downstream coverage without turning workflow into a generic graph engine.

- `macports` discovers reverse dependents and the dependency closure for each selected target against the frozen source tree. PortIndex lookup, direct reverse indexing, and transitive closure queries are implemented. `macports/dependents` now stages that index against frozen source and evaluates a deterministic coverage cohort, retaining per-target failures and index gaps; workflow still needs to invoke discovery for the prepared source outside a write transaction and persist the result.
- `prepare` proposes any required revision edits separately from the verification coverage plan.
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
- Add a project license before distribution; this remains a release prerequisite even though it is independent of the implementation order above.

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
- Tart verification with shared capacity, result reuse, retained diagnostics, and garbage collection;
- base and full-Xcode Tart provisioning through `setup`, with automatic profile selection;
- capacity-aware validation of provisioned and custom Tart images, with immutable-digest caching and reusable environment evidence;

## Review triage

This ordering incorporates the findings that remain useful from Claude's four project reviews and workflow review. Earlier findings about whole-state Git-ledger writes, the SQLite migration ladder, repeated phase inference, state/workflow policy ownership, missing CI, workflow file organization, shared Tart mechanics, PortIndex placement, and the unused placeholder planner have already been addressed; they are not new pending work.

The remaining test-placement, mechanism-documentation, exported-surface, dependency-checking, and status-contract suggestions are represented above. Cross-repository evidence reuse, requester provenance, review controls, and PR observation retain their existing design/planning slots. Do not split workflow merely because it is large, reintroduce the discarded Git ledger, rename the user-selected environment variables, or require v1 feature parity as a prerequisite for this queue.
