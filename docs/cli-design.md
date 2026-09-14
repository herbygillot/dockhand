# dockhand CLI design

See [architecture](architecture.md) for driver ownership and recovery, [principles](principles.md) for the design commitments, and [state.md](state.md) for the shared database contract. The SQLite migration and `--db` flag are implemented. `verify`, job-ID `wait`/`cancel`, and current-process `start` are implemented. Explicit version bumps and revision bumps support previews and durable preparation jobs. Automatic selection is implemented for the bounded GitHub conventions described below. Working-tree verification and matching-evidence reuse are implemented. Standalone and combined bump/publication of verified branches, resource retention, and database backup/check commands are implemented. Broader target selection remains unfinished.

## Global options

`--db PATH` selects the state database, defaulting to `$HOME/.dockhand/state.db` across all checkouts. Both `--db PATH` and `--db=PATH` work before or after the command. The `--` separator ends option parsing. Relative paths resolve against the invocation's working directory, and an explicitly empty path is rejected. Accept a filesystem path, not SQLite URI options. No short alias is assigned. The old `--lock-dir`, `-L`, and `--lockfile` flags are rejected.

```sh
dockhand --db /path/to/state.db status
dockhand verify jq --db=/path/to/state.db
```

Dockhand has no config-directory setting and does not consult `DOCKHAND_CONFIG_DIR`. A writable state operation creates a missing parent directory and database and registers the selected repository. Help, completion generation, and previews do not open state. Status uses read-only access: an absent database or unregistered repository yields empty results without creating either. Help displays the resolved file path; flag completion selects files.

One database can hold work for many repositories. Workflow commands operate on the selected checkout's registered repository; linked worktrees share that entry, while separate clones are distinct. `status` and `start` initially cover the selected repository, with no implicit all-database scope. Cooperating drivers must use the same database to coordinate shared work and resources.

## Command parsing and help

The initial command tree uses Cobra v1.10.2, matching v1, with pflag v1.0.10. `--db` and `--json` are inherited global flags. Waiting, tracing, publication, verification skipping, and preview flags are registered on the commands that support them. Cobra validates argument counts, unknown commands/flags, and the declared incompatible flag groups before the command handler constructs repository services. Help output remains ordinary text even when `--json` is present.

`dockhand help <command>` and `<command> --help` show generated command help. `usage` is an alias for `help`, including nested paths such as `dockhand usage review accept`. `dockhand completion` generates shell completion scripts through Cobra. Help and completion do not open state or require a Git repository or provider, and create no directories or files.

`status` calls the shared workflow projection through read-only SQLite access and renders human-readable output or JSON. Verification submission, job-ID attachment/cancellation, and resident execution now use the shared Go workflow API. Version and revision bumps use the shared driver; previews use the same preparation capability without opening state. Other phase-one command handlers still return explicit not-implemented errors. The broader selector syntax below remains the intended design; the concrete first slice is specified next.

## Maintenance commands

```text
dockhand gc [--older-than 168h] [--dry-run]
dockhand db backup <file>
dockhand db check
```

`gc` selects the current repository's terminal resources, releases old retained VMs, and removes diagnostic files whose confirmed release is also old. Future retention deadlines and live claims are respected; active or unresolved attempts are excluded. Dry-run performs no writes or provider calls. History, evidence, build outputs, provider identities, and lockfiles survive cleanup. Action reports support `--json`; incomplete selected actions return an error. An absent database or unregistered repository yields no eligible cleanup.

`db backup` and `db check` have an explicit whole-database scope and require no checkout or provider configuration. Backup creates a checked standalone snapshot including committed WAL contents; an existing destination is refused. Check is read-only and covers SQLite integrity and foreign keys. These two commands accept older supported schemas without migration. Missing databases are errors. They do not repair or restore a live database or resume work. See [operations and recovery](operations.md) for examples and external-state limitations.

## Implemented verification commands

```text
dockhand verify [port] --image <prepared-local-image> [--branch <branch>]
    [--subport <name>] [--variant +name|--variant=-name ...]
    [--capacity <positive-limit>] [--tests declared|skip]
    [--from-source] [--fresh] [--wait|--trace]
dockhand wait <job_id> [--trace]
dockhand cancel <job_id> [--reason <text>] [--wait]
dockhand start
```

`verify` resolves one snapshot-relative port directory/Portfile or unique directory name. Omitting `--branch` captures current working-tree contents; detached HEAD is supported when a HEAD commit exists. Supplying `--branch`, even the current branch name, selects committed contents. Output identifies the input kind, branch or detached source, HEAD/commit, modified-file count, selected target, and accepted tree. Explicit subports and variants use the existing snapshot evaluator. Standalone verification records source and targets without creating a tracked contribution. A tracked branch can verify other ports without changing its recorded set of edited ports. Omitting the port infers the single target of an open tracked contribution, as specified below. Multi-target selectors remain future work.

The prepared image is currently selected explicitly with `--image` (or through the Go application's configured default). General Git configuration loading remains separate work. The effective provider settings and image digest are recorded in the job, so queued and admitted work can resume without repeating image-selection flags. The shared pool's capacity is initially two; `--capacity` may establish another positive limit. An existing pool's limit and directory must agree. Omission reuses the recorded limit. Image availability and platform checks are distinct from admission capacity.

At initial planning, the driver looks for reusable evidence in the selected repository. A conclusive pass must cover the same complete tree, target/subport, variants, platform, image digest, verifier implementation, source-build/test policy, provider settings, and artifact inputs. A commit or revision ID can change while the tested tree stays identical. A match settles verification with an original-attempt reference, without consuming VM capacity or creating another attempt. Status and JSON retain that reference and original evidence; `--trace` reports reuse without replaying an old build log.

`verify --fresh` requires a new execution even when a pass applies. This choice is recorded at acceptance and survives detachment. `wait` and `start` continue the recorded choice. A newer terminal attempt with matching inputs and a negative or inconclusive result prevents fallback to an older pass. Lookup checks the latest 32 terminal attempts for the tree and target, so older applicable evidence can conservatively be missed. A miss reports the relevant differences and runs a build. Legacy results without a verifier identity are not reused. Image selection and source binding still run before acceptance; reuse skips driver provider calls and admission, not intake validation. Prepared bumps use this same reuse policy when they reach verification. For a combined publication job, reuse satisfies the admission milestone and leaves publication pending; only confirmation completes the job.

`--from-source` defaults to false for `verify`, `bump`, and `bump-revision`. MacPorts may use available binary archives for the target and its dependencies. Explicit `--from-source` passes MacPorts’ global `-s` option, requiring source builds for ports that need installing; it does not rebuild dependencies already installed in the VM image. The effective choice is recorded at acceptance, so a changed CLI default does not alter existing jobs.

Default verification waits for admission or a conclusive outcome; `--wait` follows completion, and `--trace` adds log streaming to stderr. `wait` and `cancel` initially take an exact job ID in the selected repository. `cancel` records intent and runs one cycle; `cancel --wait` continues until settlement. Canceling completed work preserves its existing evidence. `start` advances all eligible work in the selected repository until interrupted, without submitting new jobs or acquiring a singleton driver lock.

JSON verification/attachment results contain the selected job ID, any acceptance receipt, the last status snapshot, and an interruption indicator. Progress and logs stay on stderr. `start --json` writes a stopped/interrupted result when it exits. Exit codes distinguish milestone success (0), failed work (2), needs-attention or superseded work (3), canceled work or process interruption (130), and other errors (1). Confirmed cancellation is success for `cancel --wait`; stopping attachment never submits a cancellation request.

## Implemented preparation groundwork

```text
dockhand bump-revision <port> --diff [--branch <branch>]
    [--subport <name>] [--variant +name|--variant=-name ...]
    [--reason <text>] [--json]
dockhand bump <port> [version]
```

The revision preview selects committed source from the current local branch or explicit `--branch`. It reports that working-tree edits are excluded. It materializes the complete tree, evaluates the original Portfile and all its subports, proposes a focused revision edit, and evaluates the candidate tree. A selected subport can change without its siblings changing. A shared revision edit that changes unselected siblings, changes other evaluated metadata, or fails evaluation is refused. Revision expressions and ambiguous or dynamically named scopes remain unsupported by this first editor; versions may still be calculated because MacPorts evaluates them.

Successful previews render a Git diff to stdout and source/target information to stderr. JSON returns the selected branch, preparation result and evaluations, commit intent, and diff. Preview writes immutable Git objects as needed, but creates no branch, commit, job, database, or verification environment and leaves the user's checkout/index alone. The driver uses this same preparation service for accepted version- and revision-bump jobs.

### Version bumps

```sh
dockhand bump jq --diff
dockhand bump jq --image dockhand-base-tahoe --wait
dockhand bump jq 1.8.1 --diff
dockhand bump jq jq-1.8.1 --no-verify
dockhand bump jq 1.8.1 --image dockhand-base-tahoe --wait
```

An explicit version or tag is resolved against GitHub tags. The evaluated GitHub PortGroup prefix and suffix supply the inferred tag; no generic `v` is removed or added independently of that convention. Missing, ambiguous, and failed lookups are distinct errors. Explicit selection can choose an older version; an unchanged version is refused. Omitting the version requests automatic selection. Standalone checksum refresh remains unfinished.

Automatic selection initially requires a stable numeric current version and the evaluated GitHub tags `livecheck` convention: `livecheck.type regex`, the matching repository's `/tags` URL, and `livecheck.version` equal to the port version. An unset `github.tarball_from`, `archive`, and `tarball` select upstream repository tags without consulting GitHub Releases. Only `github.tarball_from releases` selects published GitHub Releases, excluding drafts and marked prereleases. Release metadata does not veto a tag in tag mode. Both modes exclude tag versions containing anything besides digits and dots. Explicit versions remain available for prereleases and other spellings.

Dockhand applies the evaluated Tcl regex to candidate GitHub archive URLs and orders matching versions with MacPorts `vercmp`. This respects supported version-line filters; it does not execute arbitrary livecheck scripts or scrape HTML. Custom livecheck sources and expressions requiring surrounding page content are outside this first implementation. A complete catalog and one unambiguous newest eligible tag are required. HTTP failures, incomplete pagination, no matches, and ambiguous candidates produce unknown/needs-attention, never an already-current result. Catalogs use `go-github` iterators with their default options and follow pagination to completion. Dockhand adds no catalog item/page ceilings, page-size overrides, response-size limits, or adapter/discovery timeouts. Caller cancellation and workflow call deadlines still apply.

When the newest eligible version is equal to or older than the accepted source version, the job completes successfully with an already-current detail and a recorded release observation. It creates no branch, change, or verification attempt; an unavailable image does not prevent this no-op. A preview emits no diff and opens no database. `wait` reattaches to the completed job without rediscovering versions. This records the result of that invocation, not continuing proof that upstream is unchanged. See the [automatic-selection report](activity/2026-09-13-automatic-version-selection.md).

The editor supports a literal `version` or literal version argument in `github.setup` or GitHub-backed `go.setup`; a literal declaration feeding `$version` or `${version}` into either setup also works. The evaluated GitHub version must match the port version. Preparation resets a matching literal revision to zero, downloads one archive from one direct HTTP(S) master site, and rewrites one literal unnamed checksum list with required sha256 and optional rmd160/size. The primary port is selected; sibling metadata and dependencies must remain unchanged. The Go PortGroup’s recognized toolchain compatibility pre-check is allowed during archive preparation; MacPorts executes it during verification. Unknown pre-fetch hooks, any post-fetch hooks, and replaced fetch implementations require additional preparers. Calculated version sources, multiple or named distfiles/checksums, patchfiles, and vendored sources remain unsupported. Evaluation and fidelity apply to the selected platform and variants.

The driver records the resolved tag and commit before downloading, then checks the tag before and after preparation. A moved or missing tag requires attention. Previews report the release and diff without opening state; normal jobs create `dockhand/bump/<port>-<job suffix>` through the same integration machinery as revision bumps. `--no-verify`, `--image`, `--capacity`, `--tests`, `--from-source`, `--wait`, `--trace`, `--reason`, `wait`, and `start` have the same meanings on both bump paths. Publication remains unsupported. See the [implementation report](activity/2026-09-13-explicit-version-bumps.md).

### Revision-bump jobs

```sh
dockhand bump-revision jq --no-verify
dockhand bump-revision jq --image dockhand-base-tahoe
dockhand bump-revision jq --image dockhand-base-tahoe --wait
```

The source defaults to the current local branch; `--branch` selects another committed source branch. Each request creates a new contribution branch named `dockhand/revbump/<port>-<job suffix>`. The source branch, checkout, and index stay in place. Git author identity, source commit/tree/base, target, platform, and any verification configuration are captured before acceptance. `--reason` becomes the commit body. The output reports the job, prepared branch, commit, and result revision.

`--no-verify` completes once the branch is recorded. Otherwise the command prepares the branch and waits for verification admission; `--wait` follows completion and `--trace` also streams logs. Revision bumps accept the same image, capacity, test, and source-build options as `verify`. A missing image or failed provider setup is recorded as needs-attention after preserving the branch. A later `verify <port> --branch <prepared-branch> --image <image>` creates a new verification job. `--publish` continues the same job through publication of the verified prepared revision, using the destination options of standalone `publish`. It requires verification; `--no-verify --publish` is rejected before submission.

`wait <job_id>` and `start` resume recorded preparation and integration as well as verification. Cancellation before integration leaves no output branch. After integration may have started, the driver reconciles the recorded candidate and preserves any confirmed branch. An interrupted integration whose branch is absent or contains another commit requires attention; it never overwrites user work or guesses that a deleted branch should be recreated. See the [driver implementation report](activity/2026-09-13-revision-driver.md).

## Approved source selection and human edits

Working-tree capture, standalone verification, and single-target inference from a tracked contribution are implemented. Broader target selectors and the wait/cancel branch selectors below remain future work.

When the port is omitted, the selected branch must belong to an open contribution with one recorded target. Its Portfile, evaluated name, subport, and variant choices supply the default. `--subport` selects another subport in that Portfile; explicit `--variant` choices override matching recorded choices while preserving the rest. Supplying a port explicitly starts from its defaults and supplied flags, without inheriting the contribution target's choices.

Inference compares the whole selected source tree with the recorded contribution base. Every changed path must stay within that port directory; another port, shared resources, or unrelated repository edits require an explicit port. Rename detection is disabled so both sides of a move count. An unavailable base or changed evaluated target identity also requires explicit selection. This conservative first rule can require an explicit port after a rebase introduces upstream changes elsewhere. Untracked branches, detached checkouts, and zero/multiple recorded targets never guess from HEAD or the latest job.

The CLI identifies inferred selection and displays effective variants before acceptance. Binding freezes the source and resolved target; acceptance rechecks contribution/revision identity and the recorded target used for inference. If another process changes the target without changing its revision, acceptance still fails rather than silently adopting a different scope. The driver receives an explicit target and uses ordinary verification/reuse.

The branch is the everyday handle for a tracked contribution; job IDs identify exact executions. Users can edit, commit, and rebase with ordinary Git commands, then ask Dockhand to verify or publish without a separate adoption step for every edit. A renamed or missing tracked branch requires an actionable error rather than silent reassociation.

| Command | Source and target selection |
| --- | --- |
| `bump <port\|selector> [version]` | Prepare the requested update; omission of the version requests the latest eligible release. Explicit version resolution is specified below. |
| `verify <port\|selector>` | Test the selected targets using a frozen snapshot of the current checkout, including working-tree edits. |
| `verify` | Use the current checkout and infer intended verification targets from its tracked contribution when that scope is clear. |
| `verify [<port\|selector>] --branch <branch>` | Test committed contents of the named branch; infer targets only when its tracked contribution supplies a clear scope. |
| `publish [--branch <branch>]` | Publish committed contents of the selected tracked contribution, defaulting to the current branch. |
| `wait [<job_id>] [--branch <branch>]` | Attach to existing work selected by job ID, branch, or the current contribution. |
| `cancel [<job_id>] [--branch <branch>]` | Request cancellation of existing work selected the same way. |

Port selectors and branches occupy distinct argument positions: a positional verification target is never guessed to be a branch. An explicit job ID and `--branch` are alternative selectors. Omission is allowed only when context determines the work; otherwise report the ambiguity and the concrete targets or job IDs the user can choose. Wait/cancel selection binds the relevant existing jobs at invocation time and does not follow future submissions.

Current-checkout verification captures the working-tree contents of tracked files, including deletions and staged additions, without altering the user's index or making a commit on their branch. Where a staged file has further unstaged edits, its working-tree contents are selected. Initially, new files must be staged to be included; report relevant untracked files with instructions to stage them rather than silently omitting a required patch. Explicit `--branch`, including the current branch's name, selects only committed contents. Capture includes raw working bytes, symlink targets, and executable modes without running Git clean/smudge filters or rewriting the real index. Nonignored untracked files under the selected port, any modified port directory, or shared `_resources` require staging before submission. Other untracked files are excluded. Sparse/skip-worktree entries, unresolved conflicts, submodules, unsupported file types, and individual files larger than 128 MiB are refused by this initial capture path; committed `--branch` verification remains available where its existing materializer supports the source.

Capture reads tracked contents twice and rechecks index entries and HEAD. A detected change requires a retry. This catches ordinary concurrent editing but is not an atomic filesystem snapshot. Once accepted, the Git tree is immutable; the driver never recaptures the checkout. Dirty inputs have no source commit, and the observed HEAD is recorded separately as provenance. Clean inputs retain their matching commit. No synthetic commit or hidden ref is created.

Before expensive work, display the branch or detached source, whether input comes from the working tree or a commit, the number of modified files captured, and the verification targets. Freeze the accepted snapshot before submission. Subsequent edits, commits, or branch movement cannot change a queued or running build. Reattaching to its job continues the original accepted work.

Verification evidence describes the tested source tree and build inputs. Committing an unchanged tested tree can preserve applicable evidence even though the commit ID changes. Publication selects committed source and checks the full tree, target/configuration coverage, environment, and other build inputs; matching just a Portfile, version, or commit message is insufficient. If the tested edits remain uncommitted, tell the user to commit them before publishing. Report when committed contents differ from the tested snapshot and need verification.

A contribution may change several ports. The edited-port set and the verification-target set are distinct: dependents can need testing without edits, and edits can alter the required coverage. Remember the user's intent and reconcile later source changes with it. Additional unrelated ports or shared PortGroup edits can require an explicit scope decision rather than silent inclusion or exclusion.

Standalone verification does not establish an exclusive contribution association for its branch. Running `verify jq` and then `verify terraform` on `master` must work independently. Both jobs retain exact source and target identities without turning `master` into a one-port contribution.

## The flow

The standard workflow for `dockhand` involves bumping a port's version to its latest release by default, bumping its revision, or refreshing its checksums. This produces a Git branch with the proposed changes. Build verification of these changes is requested by default unless `-N` / `--no-verify` is specified. If `-P` / `--publish` is specified, the branch will ultimately be submitted as a pull request against [macports/macports-ports](https://github.com/macports/macports-ports).

The driver owns each accepted job and its bookkeeping. The CLI submits the request transactionally to the state store through the shared workflow API, runs targeted driver cycles in the same invocation, and observes recorded progress. A normal invocation remains attached until the verification provider accepts the build; `--wait` remains attached until the requested work completes. With `--wait`, the invocation keeps running the required cycles through completion. After it exits, further workflow advancement requires a running `dockhand start` process or a later driver cycle. Both modes use the same workflow implementation; commands do not launch background drivers.

```text
dockhand bump <port|selector> [version] [-N|--no-verify] [-P|--publish] [--wait]
dockhand (bump-revision | refresh-checksums) <port|selector> [-N|--no-verify] [-P|--publish] [--wait]
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

# Request publication of the current contribution, or select a branch.
dockhand publish [--branch <branch>] [--wait]

# Verify current-checkout edits; omit targets when contribution scope is clear.
dockhand verify [<port|selector>] [--wait|--trace]

# Explicit branch selection verifies committed contents.
dockhand verify [<port|selector>] --branch <branch> [--wait|--trace]

# Read verification progress, publication state, and anything needing attention.
dockhand status

# Reattach until the selected job or jobs reach their requested destination.
dockhand wait [<job_id>] [--branch <branch>]

# Explicitly cancel outstanding work while preserving the change branch.
dockhand cancel [<job_id>] [--branch <branch>]

# Run a resident driver that advances accepted work.
# Ongoing PR monitoring is added in phase two.
dockhand start
```

The examples using `bump` flags also apply to `bump-revision` and `refresh-checksums` where meaningful. The optional version immediately follows the `bump` target. Other command-specific arguments, such as a revision-bump reason, remain separate from workflow options.

**Explicit versions and upstream tags**

```sh
# Omission requests the latest eligible release.
dockhand bump jq

# Supply the requested version with its upstream prefix.
dockhand bump jq v1.8.1

# Infer an omitted prefix from this port's current upstream reference.
dockhand bump jq 1.8.1
```

The version argument expresses the requested release; it need not be the literal text eventually written to the Portfile's `version` field. Keep the user's input, the resolved MacPorts version, and the upstream tag/reference distinct. For example, if the port currently derives tags as `v${version}`, input `1.8.1` can resolve to tag `v1.8.1` while the Portfile version remains `1.8.1`. These examples illustrate the command syntax, not a claim about jq's actual upstream tag spelling.

Preserve a prefix the user explicitly supplies. If the prefix is omitted, use the current Portfile's evaluated upstream reference and its version-to-tag convention to infer the candidate; do not hardcode `v` or derive the convention from the version field alone. Confirm the candidate against upstream release/tag evidence before applying edits. Do not double a prefix, strip arbitrary text, silently substitute another release, or treat failed upstream lookup as proof that a tag does not exist. If the convention is unclear, multiple candidates match, or no candidate can be confirmed, report the candidates or lookup failure and ask for an exact reference where that resolves the problem.

Display the resolved version and tag when they differ, including in `--diff` output. Ports using release archives without Git tags resolve the requested version through their source convention without inventing a tag. Automatic latest-release selection and explicit-version resolution share this preparation path. Once resolved for accepted work, record the chosen source identity so retries do not rediscover a different release.

**Return behavior and capacity**

| Request | When the CLI returns |
| --- | --- |
| Verification, without `--wait` | After the provider admits the initial build, or an earlier conclusive result is available. |
| Verification when the provider is at capacity | It reports that it is waiting, stays attached, and submits when capacity becomes available. |
| Verification with `--wait` | After the requested verification finishes or requires attention. |
| Verification and publication, without `--wait` | After initial build admission or evidence reuse; later settlement and publication require `wait` or a persistent driver. |
| Verification and publication with `--wait` | After publication completes or verification/publication requires attention. |
| `--no-verify`, without publication | After the driver records completion of branch creation. There is no provider-admission milestone. |
| `--no-verify --publish` | Rejected before submission; publication requires passing verification. |

For an existing change, standalone `publish` requires matching passing evidence and returns after driver pickup by default. Scheduling missing verification or joining an active build from this command remains later work. Combined `bump --publish` performs its own verification or reuses matching evidence before publication.

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

Publication requires passing verification. If verification is unavailable, the job records the setup requirement and preserves the prepared branch. `--no-verify` can request branch creation on its own; combining it with `--publish` is rejected. A negative build result is never a pass.

**Observation, previews, and results**

`status` reads driver-maintained state. It shows each target's revision, verification state, publication state, status of its associated pull request, and any blocker or setup requirement. Outstanding resource cleanup remains visible separately from the job outcome. Include the last observation time so stale information is visible. It does not take over bookkeeping when no driver is running. PR monitoring and forge-state refresh belong to the driver.

`status` selects the current repository within the chosen database and reads a consistent view of its jobs and related records. Outstanding cleanup is included even after jobs finish. Snapshot-read time is separate from evidence and PR observation times. Human output escapes embedded control characters. JSON uses the typed `workflow.Status` projection, with empty collections represented as arrays. Missing database or repository registration produces empty status; unreadable, corrupt, or unsupported state is an error. Status does not initialize or migrate the database, register repositories, or mutate workflow records. Publication and PR persistence arrive with that executor.

`--diff` performs only the preparation needed to show the proposed changes. It may evaluate Portfiles and fetch inputs needed to calculate checksums, but it does not edit the working tree, create a branch, persist a job, start a build, or publish a PR. Reject combinations with `--publish`, `--wait`, or `--trace` that ask a preview to execute the workflow.

Targets resolve consistently within the selected repository across commands. A foreign-repository job ID is an error, even if it exists in the same database. A job ID identifies exact accepted work. Port selectors identify targets, `--branch` selects committed branch source or its associated work, and job IDs select exact executions; ambiguous inferred scope produces a choice rather than silently selecting unrelated work. Selector results remain individually visible. Verification of an untracked port captures its source context so its result names what was actually tested.

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
| `verify --branch <branch>` | Use the existing verification workflow to test the selected committed revision after corrections; plain `verify` includes current-checkout edits. |
| `publish --branch <branch>` | Use the existing publication workflow to update the associated PR with the selected committed revision and applicable evidence. |

`outdated` shares discovery and version assessment with `bump`. Automatic latest-version bumps already skip current ports, so no separate `--outdated` filter is needed. Unknown results remain visible and do not prevent independent known updates from proceeding.

`rebase` and `amend` follow the established `--no-verify`, `--publish`, and `--wait` conventions. For example, `dockhand amend <branch> --publish --wait` incorporates selected corrections, verifies the resulting revision, and updates the existing PR. Verification alone does not publish local corrections. Neither command silently includes unrelated working-tree edits.

`status` distinguishes the local revision, evidence applicable to it, the last confirmed published revision, and the latest recorded PR observations. In phase two, `start` refreshes PR state, remote CI checks, review decisions, and conflict information. Before a field has been observed, it is unknown; an old observation is shown with its age. Observations do not automatically trigger corrective work.

`--wait` and `wait` continue to follow the selected job's requested destination. Publishing still completes when the PR is opened or updated, even when the change remains under review. They do not become indefinite waits for PR approval or merge. Later rebase, correction, verification, and publication requests create new jobs associated with the same tracked change.

## Implemented standalone publication (2026-09-13)

`publish [--branch <branch>] [--remote <remote>] [--upstream <remote>] [--base <branch>] [--dry-run] [--wait]` is now connected to the shared driver. It takes no port argument. Omitted `--branch` uses the current branch's committed contents, even if the checkout contains uncommitted edits. The command shows its bound commit and evidence. `--dry-run` performs local/remote preflight and renders the plan; it accepts no job and performs no remote write. It reads verification state through normal DB service initialization.

The initial executable scope is one contribution commit in one port directory, with passing evidence already recorded for its complete tree/target and selected configuration. Standalone publication tells the user to verify first when evidence is missing or not passing. Combined `bump --publish` uses the shared preparation and verification path before publication. For a user-created branch, the changed port directory selects the latest terminal verification for the exact tree. Its recorded target, subport, variants, and configuration appear in the plan and status. Publishing adopts that branch in the same transaction as the publication request. Neither verification nor `--dry-run` adopts it. Root commits, merges, multiple unpublished commits, empty changes, and changes outside one port directory are refused.

The default push remote is `origin`. PR target discovery prefers a configured `upstream` remote, otherwise the push repository's fork parent, otherwise the push repository. The target's default branch supplies the base unless overridden. The commit supplies the PR title and initial body; an existing PR's body remains intact. The API uses `GH_TOKEN` or `GITHUB_TOKEN`, while Git authentication stays with Git.

Without `--wait`, the command runs a driver cycle and returns after pickup or an earlier conclusive outcome. `--wait` stays through confirmation of the pushed head and PR metadata. `wait <job_id>`, `start`, cancellation, JSON output, and detachment use the existing workflow path. An uncertain issued PR request is observed without another write; it can remain pending when the remote outcome cannot be established. `status` shows that state and the retained PR URL after confirmation.

## Implemented combined bump and publication

```sh
dockhand bump jq [version] --publish --image dockhand-base-tahoe [--wait|--trace]
dockhand bump-revision jq --publish --image dockhand-base-tahoe [--wait|--trace]
```

Both accept `--remote`, `--upstream`, and `--base` with the same defaults as `publish`. On bump commands those flags require `--publish`. Destination repositories, URLs, base branch, and the operation-lock directory are frozen before acceptance. No remote ref or PR is written during intake.

One job owns preparation, verification, and publication. Its accepted input source remains the original commit; its result revision identifies the prepared commit that is built and published. A passing build or applicable reuse leaves the job active for publication. The driver then records remote preconditions and content before any push, using the same executor and uncertainty handling as standalone publication. Status shows the requested destination before that publication checkpoint exists.

Without `--wait` or `--trace`, the command waits through capacity pressure and returns at build admission, evidence reuse, or an earlier terminal outcome. `wait <job_id>` and `start` resume the exact accepted destination without repeating flags. With `--wait` or `--trace`, completion means PR confirmation. An automatic no-update result needs no branch, verification, or PR.

Failed verification prevents publication. A changed or missing prepared branch or newer matching negative evidence stops fresh remote effects; human edits require a new explicit verification/publication request. Cancellation before a PR request preserves any branch already created or pushed. Once the PR request has started, the existing observation-only recovery applies. This slice adds no unverified publication override, automatic rebase/squash, downstream scheduling, or post-PR monitoring.
