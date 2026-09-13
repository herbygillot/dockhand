# dockhand CLI design

See [architecture](architecture.md) for driver ownership and recovery, [principles](principles.md) for the design commitments, and [state.md](state.md) for the shared database contract. The SQLite migration and `--db` flag are implemented. `verify`, job-ID `wait`/`cancel`, and current-process `start` are implemented. Preparation, publication, and broader target selection remain unfinished.

## Global options

`--db PATH` selects the state database, defaulting to `$HOME/.dockhand/state.db` across all checkouts. Both `--db PATH` and `--db=PATH` work before or after the command. The `--` separator ends option parsing. Relative paths resolve against the invocation's working directory, and an explicitly empty path is rejected. Accept a filesystem path, not SQLite URI options. No short alias is assigned. The old `--lock-dir`, `-L`, and `--lockfile` flags are rejected.

```sh
dockhand --db /path/to/state.db status
dockhand verify jq --db=/path/to/state.db
```

Dockhand has no config-directory setting and does not consult `DOCKHAND_CONFIG_DIR`. A writable state operation creates a missing parent directory and database and registers the selected repository. Help, completion generation, and previews do not open state. Status uses read-only access: an absent database or unregistered repository yields empty results without creating either. Help displays the resolved file path; flag completion selects files.

One database can hold work for many repositories. Commands operate on the selected checkout's registered repository; linked worktrees share that entry, while separate clones are distinct. `status` and `start` initially cover the selected repository, with no implicit all-database scope. Cooperating drivers must use the same database to coordinate shared work and resources.

## Command parsing and help

The initial command tree uses Cobra v1.10.2, matching v1, with pflag v1.0.10. `--db` and `--json` are inherited global flags. Waiting, tracing, publication, verification skipping, and preview flags are registered on the commands that support them. Cobra validates argument counts, unknown commands/flags, and the declared incompatible flag groups before the command handler constructs repository services. Help output remains ordinary text even when `--json` is present.

`dockhand help <command>` and `<command> --help` show generated command help. `usage` is an alias for `help`, including nested paths such as `dockhand usage review accept`. `dockhand completion` generates shell completion scripts through Cobra. Help and completion do not open state or require a Git repository or provider, and create no directories or files.

`status` calls the shared workflow projection through read-only SQLite access and renders human-readable output or JSON. Verification submission, job-ID attachment/cancellation, and resident execution now use the shared Go workflow API. Other phase-one command handlers still return explicit not-implemented errors. The broader selector syntax below remains the intended design; the concrete first slice is specified next.

## Implemented verification commands

```text
dockhand verify <port> --image <prepared-local-image> [--branch <branch>]
    [--subport <name>] [--variant +name|--variant=-name ...]
    [--capacity <positive-limit>] [--tests declared|skip]
    [--from-source=false] [--wait|--trace]
dockhand wait <job_id> [--trace]
dockhand cancel <job_id> [--reason <text>] [--wait]
dockhand start
```

`verify` resolves one snapshot-relative port directory/Portfile or unique directory name. The current literal local branch is the default; detached HEAD requires `--branch`. The input is committed source, and the accepted commit ID is reported. This does not adopt uncommitted edits. Explicit subports and variants use the existing snapshot evaluator. A tracked branch currently has an accepted target association; an unrelated target on the same branch is rejected. Branch-only inference and multi-target selectors remain future work.

The prepared image is currently selected explicitly with `--image` (or through the Go application's configured default). General Git configuration loading remains separate work. The effective provider settings and image digest are recorded in the job, so queued and admitted work can resume without repeating image-selection flags. The shared pool's capacity is initially two; `--capacity` may establish another positive limit. An existing pool's limit and directory must agree. Omission reuses the recorded limit. Image availability and platform checks are distinct from admission capacity.

Default verification waits for admission or a conclusive outcome; `--wait` follows completion, and `--trace` adds log streaming to stderr. `wait` and `cancel` initially take an exact job ID in the selected repository. `cancel` records intent and runs one cycle; `cancel --wait` continues until settlement. Canceling completed work preserves its existing evidence. `start` advances all eligible work in the selected repository until interrupted, without submitting new jobs or acquiring a singleton driver lock.

JSON verification/attachment results contain the selected job ID, any acceptance receipt, the last status snapshot, and an interruption indicator. Progress and logs stay on stderr. `start --json` writes a stopped/interrupted result when it exits. Exit codes distinguish milestone success (0), failed work (2), needs-attention or superseded work (3), canceled work or process interruption (130), and other errors (1). Confirmed cancellation is success for `cancel --wait`; stopping attachment never submits a cancellation request.

## The flow

The standard workflow for `dockhand` involves bumping a port's version to its latest release by default, bumping its revision, or refreshing its checksums. This produces a Git branch with the proposed changes. Build verification of these changes is requested by default unless `-N` / `--no-verify` is specified. If `-P` / `--publish` is specified, the branch will ultimately be submitted as a pull request against [macports/macports-ports](https://github.com/macports/macports-ports).

The driver owns each accepted job and its bookkeeping. The CLI submits the request transactionally to the state store through the shared workflow API, runs targeted driver cycles in the same invocation, and observes recorded progress. A normal invocation remains attached until the verification provider accepts the build; `--wait` remains attached until the requested work completes. With `--wait`, the invocation keeps running the required cycles through completion. After it exits, further workflow advancement requires a running `dockhand start` process or a later driver cycle. Both modes use the same workflow implementation; commands do not launch background drivers.

```text
dockhand (bump | bump-revision | refresh-checksums) <port|selector> [-N|--no-verify] [-P|--publish] [--wait]
```

```sh
# Check the required tools and optionally provision verification environments.
dockhand setup

# Create a change and request verification. Wait for provider admission,
# then return. Later bookkeeping needs dockhand start or another driver cycle.
dockhand bump jq
dockhand bump-revision jq
dockhand refresh-checksums jq

# Preview the proposed diff without creating a branch or submitting a job.
dockhand bump jq --diff

# Follow one port's build logs and remain attached through the requested work.
dockhand bump jq --trace

# Wait for verification. This does not request publication.
dockhand bump jq --wait

# Request verification followed by publication. Return after build admission.
# Publication then requires a running persistent driver or a later driver cycle.
dockhand bump jq --publish

# Remain attached through verification and publication.
dockhand bump jq --publish --wait

# Create only the branch, with verification explicitly skipped.
dockhand bump jq --no-verify

# Explicitly request publication without verification, subject to publication policy.
dockhand bump jq --no-verify --publish --wait

# Request publication of an existing change.
dockhand publish <port|selector|branch> [--wait]

# Request verification of an existing target; optionally follow its build logs.
dockhand verify <port|selector|branch> [--wait|--trace]

# Read verification progress, publication state, and anything needing attention.
dockhand status

# Reattach until the selected job or jobs reach their requested destination.
dockhand wait <port|selector|branch|job_id>

# Explicitly cancel outstanding work while preserving the change branch.
dockhand cancel <port|selector|branch>

# Run a resident driver that advances accepted work.
# Ongoing PR monitoring is added in phase two.
dockhand start
```

The examples using `bump` flags also apply to `bump-revision` and `refresh-checksums` where meaningful. Command-specific arguments, such as a target version or revision-bump reason, are additional to the workflow options shown here.

**Return behavior and capacity**

| Request | When the CLI returns |
| --- | --- |
| Verification, without `--wait` | After the provider admits the initial build, or an earlier conclusive result is available. |
| Verification when the provider is at capacity | It reports that it is waiting, stays attached, and submits when capacity becomes available. |
| Verification with `--wait` | After the requested verification finishes or requires attention. |
| Verification and publication, without `--wait` | After initial build admission; later settlement and publication require a persistent driver or another driver cycle. |
| Verification and publication with `--wait` | After publication completes or verification/publication requires attention. |
| `--no-verify`, without publication | After the driver records completion of branch creation. There is no provider-admission milestone. |
| `--no-verify --publish`, without `--wait` | After publication work is durably accepted in the database and the prepared branch is recorded. |
| `--no-verify --publish --wait` | After publication completes or requires attention. |

For an existing change, `publish` uses matching verification evidence, joins matching active verification, or requests missing verification as required by publication policy. If a build is required, its default return point is provider admission. If no build is required, its default return point is durable acceptance of the publication work. It does not repeatedly rebuild a known failed revision to avoid reporting the failure.

For a selector, the return condition applies to each selected target operation. Without `--wait`, each must reach its applicable handoff milestone or report a conclusive result. When there are more initial builds than available slots, this can require waiting for earlier builds to finish. The driver continues independent targets when another fails. Downstream follow-up builds remain the driver's responsibility after the initial admission; `--wait` follows the full requested operation.

**Waiting, tracing, and cancellation**

Requests and progress pass through the state store. Attachment means observing the selected durable jobs, not opening a socket to a driver. Successful submission returns a job ID without waiting for driver pickup; ordinary verification commands still remain present until provider admission, and `--wait` follows completion. After this invocation exits, report pending work and its job ID without promising automatic advancement when no persistent driver is running.

`--wait` changes attachment, not the requested destination. It never enables publication by itself. With verification skipped and no publication requested, it waits only for branch creation.

`--trace` has the completion behavior of `--wait` and also streams build logs. It is available for a single selected port when verification is enabled. With `--publish`, it stays attached through the publication result after the build logs end. Reject incompatible requests such as `--trace --no-verify`, or tracing a multi-port selector, with a clear usage error. Supplying both `--trace` and `--wait` is redundant and harmless.

`wait` attaches to existing work; it does not submit a fresh verification or request publication. It can run targeted cycles in the current process to resume the selected durable jobs, sharing the same engine and state claims as `dockhand start`. It binds the jobs and revisions selected when the command starts rather than silently following future requests or new branch tips.

Once a job is durably accepted in the database, Ctrl-C stops the CLI's observation, including before driver pickup or while waiting for provider capacity. Accepted work remains in the database, and submitted provider builds may continue. Further workflow advancement requires a running persistent driver or another driver cycle. Use `cancel` to ask the driver to stop outstanding verification and any pending publication continuation. Cancellation preserves the branch and completed evidence; it does not undo an already-published PR. Cancellation and resource cleanup are recorded and performed by the driver.

**Unavailable verification and explicit skipping**

Temporary capacity pressure is different from missing tools, an unprovisioned environment, or an unsupported target configuration. Capacity pressure waits for admission. A setup requirement or a preflight refusal returns an explicit result and a next action instead of waiting indefinitely for a slot that cannot become usable.

Branch creation remains useful when verification tools are unavailable. Preserve the branch and record why verification could not proceed. Distinguish that from `--no-verify`, where the user explicitly chose to skip verification. Neither case is a verification pass.

Missing verification tools must not silently authorize unverified publication. If publication needs verification and it is unavailable, the job records the setup requirement. An explicit request to skip verification follows publication policy and is recorded as such. A negative build result must never be rewritten as a pass merely because an override was requested.

**Observation, previews, and results**

`status` reads driver-maintained state. It shows each target's revision, verification state, publication state, status of its associated pull request, and any blocker or setup requirement. Outstanding resource cleanup remains visible separately from the job outcome. Include the last observation time so stale information is visible. It does not take over bookkeeping when no driver is running. PR monitoring and forge-state refresh belong to the driver.

`status` selects the current repository within the chosen database and reads a consistent view of its jobs and related records. Outstanding cleanup is included even after jobs finish. Snapshot-read time is separate from evidence and PR observation times. Human output escapes embedded control characters. JSON uses the typed `workflow.Status` projection, with empty collections represented as arrays. Missing database or repository registration produces empty status; unreadable, corrupt, or unsupported state is an error. Status does not initialize or migrate the database, register repositories, or mutate workflow records. Publication and PR persistence arrive with that executor.

`--diff` performs only the preparation needed to show the proposed changes. It may evaluate Portfiles and fetch inputs needed to calculate checksums, but it does not edit the working tree, create a branch, persist a job, start a build, or publish a PR. Reject combinations with `--publish`, `--wait`, or `--trace` that ask a preview to execute the workflow.

Targets resolve consistently within the selected repository across commands. A foreign-repository job ID is an error, even if it exists in the same database. A job ID identifies exact accepted work. Branches and unique port names provide convenient access to tracked changes; ambiguous references produce a choice rather than silently selecting unrelated work. Selector results remain individually visible. Verification of an untracked port captures its source context so its result names what was actually tested.

The default command result says what was handed off, including the job ID and destination. It does not claim that a still-running verification has passed. Attached commands return the outcome of the work they awaited, with failure and needs-attention results distinguishable from successful completion. Keep final output and progress reporting based on the same driver-maintained state.

Review actions use the `review` command family: `dockhand review accept <change>` and `dockhand review dismiss <change>`. Review decisions that alter accepted work are submitted to the state store through the shared workflow API; the driver records and performs their consequences.

**JSON Output**

All commands (except for `help` or `usage`) should be able to output JSON when given a `--json` flag.

## Phase-two discovery and follow-up workflows

The architecture must support these workflows from phase one, even though their command handlers and ongoing monitoring arrive in phase two. The command names below are the proposed interface; exact corrective-edit selection and conflict-resolution syntax remain to be specified.

| Command | Intended behavior |
| --- | --- |
| `outdated <port\|selector>` | Report eligible upstream updates, current ports, and unknown results without creating branches or workflow jobs. |
| `rebase <branch>` | Prepare a new revision of the tracked change on the latest fetched upstream base, then request verification by default. |
| `amend <branch>` | Incorporate explicitly selected corrective edits into the appropriate logical commit, then request verification by default. |
| `verify <branch>` | Use the existing verification workflow to test the selected local revision after corrections. |
| `publish <branch>` | Use the existing publication workflow to update the associated PR with the selected revision and applicable evidence. |

`outdated` shares discovery and version assessment with `bump`. Automatic latest-version bumps already skip current ports, so no separate `--outdated` filter is needed. Unknown results remain visible and do not prevent independent known updates from proceeding.

`rebase` and `amend` follow the established `--no-verify`, `--publish`, and `--wait` conventions. For example, `dockhand amend <branch> --publish --wait` incorporates selected corrections, verifies the resulting revision, and updates the existing PR. Verification alone does not publish local corrections. Neither command silently includes unrelated working-tree edits.

`status` distinguishes the local revision, evidence applicable to it, the last confirmed published revision, and the latest recorded PR observations. In phase two, `start` refreshes PR state, remote CI checks, review decisions, and conflict information. Before a field has been observed, it is unknown; an old observation is shown with its age. Observations do not automatically trigger corrective work.

`--wait` and `wait` continue to follow the selected job's requested destination. Publishing still completes when the PR is opened or updated, even when the change remains under review. They do not become indefinite waits for PR approval or merge. Later rebase, correction, verification, and publication requests create new jobs associated with the same tracked change.
