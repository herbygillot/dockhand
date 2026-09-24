# Principles

These principles preserve useful decisions from the first Dockhand and guide the new implementation. [Architecture](architecture.md) describes their internal application; [CLI design](cli-design.md) defines the command behavior. They establish responsibilities without prescribing a package for every concept.

## Evaluate through MacPorts; edit source text precisely

MacPorts evaluates Portfiles, including Tcl interpolation, procedures, calculations, and PortGroups. Dockhand uses that evaluation to understand a port, then makes focused source edits while preserving surrounding text. A syntax tree can locate an edit; it does not replace MacPorts as the authority on what the Portfile means.

## Evaluate the complete source context

A Portfile's meaning depends on its source tree, PortGroups, auxiliary files, selected subport, variants, and platform. Preparation and verification must use the intended context for both the original and proposed revision.

Edits can span the ports tree, including shared PortGroups. Each edited file has a precondition, and validation considers the contexts affected by those edits. Compare relevant before-and-after evaluations to establish that the intended change occurred without unintended sibling changes. Edit fidelity, successful builds, and complete dependency declarations are separate questions.

## Separate evidence collection from judgment

Provider, MacPorts, Git, and forge adapters collect observations. Deterministic decision logic judges those observations and the applicable policy. Keep decisions testable without running a VM or contacting GitHub.

Missing or unreadable evidence remains unknown. An explicit override records a policy decision; it does not turn failed or absent verification into a pass.

## Bind evidence to explicit inputs

Every verification attempt names the immutable source and build configuration it tests, including the selected port, variants, platform, relevant build options, and any reused dependency artifacts. Reuse results only when those inputs satisfy the new request. Temporary staging paths and whether a CLI is attached do not change the build question.

A branch is a convenient reference to work, but its name is not evidence identity. Advancing a branch must not silently transfer an earlier verdict to new contents. Record the effective configuration needed to explain and reproduce accepted work.

An executable’s reported version does not establish which source release produced it. Version text can differ from a tag, be stale, or be absent. Bind release identity to source and artifact identities; judge verification through the requested build, test, and installation behavior. Reported version text alone must not reject a port update.

## Track the contribution beyond individual jobs

A logical change has a stable identity across rebases, corrective edits, rewritten commits, and repeated verification. Its revisions and evidence retain their own identities. Opening or updating a PR completes a publication job, while the contribution remains available for later review and follow-up work. A PR association survives revision changes.

Discovery and PR monitoring produce observations that may motivate work. They do not by themselves authorize edits or publication. Keep those observation paths usable independently of submitting a job, and use the same driver for any resulting authorized work.

## Separate jobs, attempts, resources, and publication

A job represents requested work and its destination. It may require multiple build attempts, dependent builds, and a publication action. Attempts record individual executions; resources have their own ownership and retention; publication records what was requested and what the forge confirms.

Those lifetimes differ. Retrying a build preserves the earlier attempt. Completing a job can leave cleanup outstanding. Requesting publication can reuse matching evidence. Keep these distinctions explicit without requiring a separate process or package for each one.

## Give the driver responsibility for accepted work

The driver owns the durable progression of an accepted job, including waiting for capacity, submitting builds, recording evidence, making decisions, publishing when authorized, and cleaning up. The CLI submits requests to the state store through shared workflow intake functions and observes recorded progress. Durable submission is distinct from driver pickup and provider admission. The state store provides the handoff between processes; no separate request transport is needed. Waiting or tracing changes how long the CLI stays attached, not who owns the work or what publication is authorized.

Action invocations and explicit persistent mode (`dockhand serve`) use the same workflow implementation in their current process. Commands do not spawn background drivers. That is a decision of the first implementation, not a principle: claims in the store already coordinate several processes, and nothing yet needs a resident one. The [coordination note](instance-coordination.md) records what would reopen it: a peer cannot tell a dead driver's claim from a slow one's, another terminal sees progress only as recorded detail, and two databases cannot see each other's reservations. A CLI exit or driver crash must leave enough durable information for a later driver cycle to resume or report what needs attention; durable records alone do not execute pending work.

## Report observations without silently refreshing them

`status` reads durable observations. Driver cycles reconcile accepted work and refresh external facts. A snapshot timestamp describes when state was read, not when a provider or forge was last observed; expose observation age separately. Reading status does not authorize new external work.

## Keep authoritative state and recoverable effects

Store durable workflow metadata in SQLite behind backend-independent state contracts. One database can hold multiple repositories, with explicit repository scope for reads, relationships, and claims. Linked worktrees share repository identity; separate clones remain distinct. Git stores source, while database records preserve source identity and evidence. Missing source requires an availability decision, never silent replacement with a moving branch tip. The [state design](state.md) defines the initial boundary.

Record accepted work and provider/publication intent before executing it. Use short transactions that acquire claims and update related state atomically, then check the current claim and state when recording results. Recovery reconciles uncertain provider or forge actions before retrying. Interrupted Git work is inspected or reported as needing attention; it does not require a generic operation journal or atomic Git/database commit. Neither a lock nor a recorded intention alone guarantees that an external action happens only once.

## Preserve failure attribution and independent progress

Distinguish failures in the target port, blockers in dependencies, unsupported configurations, infrastructure failures, and Dockhand faults. Record the failing package and phase separately from any conclusion that the proposed change caused the failure. A dependency outside the original dependent list is not automatically unrelated to the change.

The set of ports needing revision bumps is distinct from the set that can share a build environment. Conflicting dependents can be tested separately. Continue independent work when one target fails, and report partial coverage truthfully. Publication policy decides whether the recorded coverage is sufficient; it must not invent successful outcomes for untested targets.
