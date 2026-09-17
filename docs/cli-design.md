# dockhand CLI design

See [output](output.md) for what each command prints and how `status` renders, [architecture](architecture.md) for driver ownership and recovery, [principles](principles.md) for the design commitments, [state.md](state.md) for the shared database contract, and the [roadmap](roadmap.md) for current priorities and deferred work. The SQLite migration and `--db` flag are implemented. `setup`, `verify`, job- and contribution-selected `wait`/`cancel`, and current-process `start` are implemented. Explicit version bumps and revision bumps support previews and durable preparation jobs. Automatic selection supports GitHub/GitLab catalogs and evaluated HTTP regex livecheck listings. Working-tree verification and matching-evidence reuse are implemented. Standalone and combined bump/publication of verified branches, resource retention, and database backup/check commands are implemented. Named ports and subports continue one tracked contribution across bump, verify, and publish.

## Target-oriented continuation

The [target workflow](target-workflow.md) is implemented through its ordinary-workflow and preparation-reliability milestone. `bump <target>`, `verify <target>`, and `publish <target>` accept ports and subports identically and follow one repository-scoped contribution. That identity exists before preparation succeeds. Manual checkout verification requires `--working-tree`; ambiguous contributions require `--change` or `--branch`. Automatic discovery and editing capability remain separate checks.

`abandon [target]` ends a contribution locally, with pending-job exclusion in the same transaction as disposition. `refresh [target]` observes the associated PR outside transactions and conditionally records its outcome; it retires matching completed work while preserving newer local corrections. When the PR has merged and the contribution retires, `refresh` deletes the local branch and the fork's head branch, each guarded by the published commit and by the branch not being checked out; the outcome for each is part of the result. For an open PR it also records what the forge reports about the head: draft state, mergeability with the forge's detail, the latest review from each reviewer, and check runs and commit statuses with the failing names. That status is shown by `refresh` and `status` and never acted on: no rerun, comment, push, or edit follows from it. Both support `--branch` and exact historical `--change`. They preserve branches and history, never mutate the remote PR, and never reopen retired work. See [usage](usage.md#end-a-contribution-or-refresh-its-pr). Status remains a local snapshot.

## Global options

`--tree PATH` / `-T PATH` selects the ports checkout. It defaults to `MACPORTS_TREE` when nonempty, otherwise the current directory. The selection applies to repository-scoped commands without changing the process working directory. Database-only commands still do not need a ports tree.

`--prefix PATH` / `-P PATH` selects the local MacPorts installation used for evaluation, through `<prefix>/bin/port-tclsh`. It defaults to `MACPORTS_PREFIX` when nonempty; otherwise Dockhand finds `port-tclsh` on the executable search path. The VM's MacPorts prefix remains a separate provider setting tied to its image. `--no-publish` has no short alias now that `-P` selects the prefix.

`--git PATH` selects the Git executable used for all source-repository operations. It defaults to `GIT_BIN` when nonempty, then to `git` on the executable search path. A path supplied by the flag overrides the environment and an embedding caller's configured executable. Relative paths containing a directory component resolve against the invocation's working directory; a bare executable name remains eligible for `PATH` lookup.

`--tart PATH` selects the Tart executable used for image setup and verification. It defaults to `TART_BIN` when nonempty, then to `tart` on the executable search path. It follows the same flag precedence and path-resolution rules as `--git`.

Explicit flags override the environment. The global path flags are inherited by subcommands, resolve relative values against the invocation's working directory, reject explicitly empty values, and offer the appropriate file or directory completion. Parsing and help do not check that the selected paths exist or create them.

```sh
dockhand -T ~/Source/macports-ports -P /opt/local status
MACPORTS_TREE=~/Source/macports-ports MACPORTS_PREFIX=/opt/local GIT_BIN=/opt/local/bin/git dockhand bump jq
```

`--db PATH` selects the state database, defaulting to `$HOME/.dockhand/state.db` across all checkouts. Both `--db PATH` and `--db=PATH` work before or after the command. The `--` separator ends option parsing. Relative paths resolve against the invocation's working directory, and an explicitly empty path is rejected. Accept a filesystem path, not SQLite URI options. No short alias is assigned. The old `--lock-dir`, `-L`, and `--lockfile` flags are rejected.

```sh
dockhand --db /path/to/state.db status
dockhand verify jq --db=/path/to/state.db
```

Dockhand has no config-directory setting and does not consult `DOCKHAND_CONFIG_DIR`. A writable state operation creates a missing parent directory and database and registers the selected repository. Help, completion generation, and previews do not open state. Status uses read-only access: an absent database or unregistered repository yields empty results without creating either, unless a specific job ID was requested, which returns not-found. Help displays the resolved file path; flag completion selects files.

One database can hold work for many repositories. Workflow commands operate on the selected checkout's registered repository; linked worktrees share that entry, while separate clones are distinct. `status` and `start` initially cover the selected repository, with no implicit all-database scope. Cooperating drivers must use the same database to coordinate shared work and resources.

## Command parsing and help

The initial command tree uses Cobra v1.10.2, matching v1, with pflag v1.0.10. `--tree` / `-T`, `--prefix` / `-P`, `--git`, `--tart`, `--db`, and `--json` are inherited global flags. Waiting, tracing, publication, verification skipping, and preview flags are registered on the commands that support them. Cobra validates argument counts, unknown commands/flags, and the declared incompatible flag groups before the command handler constructs repository services. Help output remains ordinary text even when `--json` is present.

`dockhand help <command>` and `<command> --help` show generated command help. `usage` is an alias for `help`, including nested paths such as `dockhand usage review accept`. `dockhand completion` generates shell completion scripts through Cobra. Help and completion do not open state or require a Git repository or provider, and create no directories or files.

`status [target]`, `status --active`, and `status --branch <branch>` call the shared workflow projection through read-only SQLite access and render human-readable output or JSON. Verification submission, fixed job- or branch-selected attachment/cancellation, and resident execution now use the shared Go workflow API. Version and revision bumps use the shared driver; previews use the same preparation capability without opening state. Other phase-one command handlers still return explicit not-implemented errors. The broader selector syntax below remains the intended design; the concrete first slice is specified next.

## Authentication roadmap

`dockhand auth login` implements GitHub's OAuth device flow without requiring `gh` or a manually copied token. It displays the one-time code, opens GitHub's verification page unless `--no-browser` is set, polls at GitHub's required interval, validates the selected account, and stores the token in macOS Keychain. Login does not require a ports checkout, open the workflow database, or create a workflow job. It requests the `public_repo` scope needed for public contribution work. See [GitHub's device-flow documentation](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps#device-flow).

Dockhand includes the public client ID for its registered GitHub OAuth application, with device flow enabled. `--client-id`, `DOCKHAND_GITHUB_CLIENT_ID`, or `make GITHUB_OAUTH_CLIENT_ID=<id>` may override it for development and alternate registrations. No client secret is used or distributed. Login replaces the existing Dockhand credential for `github.com`. Expiring access tokens remain disabled until Dockhand can retain rotated refresh tokens and coordinate refreshes across processes.

Publication authentication is implemented. An explicit credential supplied by an embedding caller takes precedence, followed by `GH_TOKEN`, `GITHUB_TOKEN`, Dockhand's Keychain credential, and the active `gh` login for `github.com`. Dockhand asks `gh auth token` only after its own credential is absent. Git pushes continue to use Git's separate credentials. A custom API origin requires an explicitly supplied credential source so credentials for `github.com` cannot be sent to another host.

`dockhand auth status` checks the selected credential against GitHub and reports its source and account. Rejections name the source and how to replace it; Dockhand does not try another identity after rejection. `dockhand auth login` replaces the saved Keychain credential. If `GH_TOKEN` or `GITHUB_TOKEN` is set, login explains that the environment credential still takes precedence.

`dockhand auth logout` removes only Dockhand's Keychain entry; an absent entry is already logged out. It does not revoke the token on GitHub or modify environment variables or `gh` credentials. A later invocation may therefore select an environment token or the GitHub CLI login. Run `dockhand auth status` to check. Both commands work without a ports checkout or database; `--json` reports source/account/status or removal outcome without a token.


An accepted standalone or combined publication performs an authenticated-user check before recording the request. The driver checks again immediately before a branch push and immediately before a PR create or update. A missing or rejected credential therefore settles as a publication precondition while the operation is still known not to have started; transient API failures remain retryable. Observation-only recovery after an uncertain PR response remains available without a fresh credential. Public API reads and `publish --dry-run` can still succeed without authenticating, so a preview does not prove that publication will succeed.

Repository and identity API calls remain in `go-github`; `golang.org/x/oauth2` owns standards-compliant device polling, including `authorization_pending` and `slow_down`. Keychain writes use the macOS `security` command's stdin command mode, so the token is absent from process arguments. Credentials do not enter accepted job records, the database, logs, or JSON results. A successful identity check does not guarantee repository permission or prevent later revocation; rejected writes and uncertain outcomes still use the existing publication recovery rules.

## Maintenance commands

Verification-capable commands accept `--keep-failed` for explicit local-VM retention. Normal cycles release terminal environments by default and prune bounded batches of old released diagnostics after seven days. Accepted policy belongs to `JobSpec`, separate from build/evidence inputs. Manual `gc` continues to release intentionally retained environments and collect reusable caches. See [routine cleanup](operations.md#routine-cleanup). It also deletes local branches of merged contributions that still hold their published commit and are not checked out, reporting each as `delete-branch`; fork branches are removed at merge observation only.

```text
dockhand gc [--older-than 168h] [--dry-run] [--all-repositories]
dockhand db backup <file>
dockhand db check
dockhand db migrate
```

`gc` selects the current repository's terminal resources, releases old retained VMs, and removes diagnostic files whose confirmed release is also old. `--all-repositories` runs the same collection over every registration in the database, needs no checkout, and names each registration it visited, marking those whose checkout no longer exists; a deleted checkout can otherwise never release its environments. Future retention deadlines and live claims are respected; active or unresolved attempts are excluded. Dry-run performs no writes or provider calls. History, evidence, build outputs, provider identities, and lockfiles survive cleanup. Action reports support `--json`; incomplete selected actions return an error. An absent database or unregistered repository yields no eligible cleanup.

`db backup` and `db check` have an explicit whole-database scope and require no checkout or provider configuration. Backup creates a checked standalone snapshot including committed WAL contents; an existing destination is refused. Check is read-only and covers SQLite integrity and foreign keys. These two commands accept older supported schemas without migration. Missing databases are errors. They do not repair or restore a live database or resume work. See [operations and recovery](operations.md) for examples and external-state limitations.

`db migrate` upgrades the selected existing Dockhand database through the normal transactional migration chain, across all repositories. It needs no checkout or external tools, creates no job, and advances no workflow. A current schema is a successful no-op; missing, empty, foreign, or newer databases are refused. Read-only status and GC preview report when an older supported schema requires this command and remind callers to reuse their `--db` option. Backup/check can inspect the old schema before migration.

## Tart setup

```text
dockhand setup [--os <release-name-or-major-version>] [--check|--rebuild] [--image <local-name>]
    [--source <oci-image>] [--macports-version <version>]
    [--xcode <archive-or-directory>]
```

`setup` prepares the local Tart image used by native verification. It determines the native Darwin version and architecture through the selected local MacPorts installation, chooses a conventional image name and matching vanilla macOS source, and provisions only when that image is missing. An existing image is started only as a disposable clone and must pass the same checks. `--check` refuses a missing image and performs no pull or installation. `--rebuild` always prepares and validates a replacement before attempting to adopt it.

`--os` selects a macOS release by name or major product version, for example `--os sonoma` or `--os 14`. It keeps the native architecture and chooses the matching source, image names, MacPorts installer, and Xcode archive. Omit it to use the host release.

The base profile installs the pinned Tart guest agent from its release archive after verifying its SHA-256 digest, installs Apple's Command Line Tools when the vanilla source lacks a working compiler, then installs the selected official MacPorts package. `--xcode` adds full Xcode from an explicit `.xip` or selects the newest compatible release archive in a directory. It creates a separate conventional image such as `dockhand-xcode-tahoe` and refuses to replace a `dockhand-base-*` image. Equal-version Apple-silicon archives are preferred over universal and unsuffixed archives. Pre-release archive names are ignored. Validation requires the requested platform and MacPorts version, guest command transport (stdin, stdout, and failure propagation), a working compiler, passwordless sudo, MacPorts Tcl packages, no active ports, and no recognized foreign package-manager prefix. A base profile must select Command Line Tools; an Xcode profile must select `/Applications/Xcode.app` and report the requested exact Xcode version. The implementation supports arm64 Darwin 21 through 25 and MacPorts under `/opt/local`.

Provisioning uses temporary `-next` images. A failed build leaves the current base and golden images unchanged. Adoption takes a per-image write lock; verification takes the corresponding read lock while hashing or cloning the base. The lock descriptor is inherited by Tart clone children, so process death cannot expose a still-running clone to replacement. Another per-image lock serializes setup commands even when their processes use different state databases. A failed final adoption retains the proven `-next` candidate, and a retained golden image restores a missing base during a later ordinary setup. These locks live under the selected Tart home and protect external VM operations; they do not authorize workflow state writes.

`setup` creates no workflow job and does not open SQLite or require a ports checkout. It reports progress on stderr and a human or JSON result on stdout. The Tart executable follows global `--tart` / `TART_BIN` selection. The default image follows the native release, such as `dockhand-base-tahoe`; supplying `--xcode` changes that default to `dockhand-xcode-tahoe`. When `--image` is omitted, verification selects the conventional Xcode image for a port that requires Xcode and the smaller base image otherwise.

`setup` reports `go2port` and `cargo2port` as optional host tools. Their absence does not fail setup. A bump or preview that needs one to regenerate a dependency/checksum block stops with a missing-tool error naming the port and block; unrelated updates do not require either executable. Global `--go2port` and `--cargo2port` flags override `GO2PORT_BIN` and `CARGO2PORT_BIN`, respectively; otherwise executables are found on PATH. Helpers run on the host in temporary preparation directories, outside state transactions. Supported literal declarations are regenerated against the source manifests and reevaluated before normal verification and publication. See [dependency preparation](dependency-preparation.md) for the supported forms and refusal cases.

## Implemented verification commands

```text
dockhand verify [target] [--image <prepared-local-image>]
    [--branch <branch> | --change <id> | --working-tree]
    [--variant +name|--variant=-name ...]
    [--capacity <positive-limit>] [--tests declared|skip]
    [--from-source] [--fresh] [--detach|--trace]
dockhand wait [target] [--job <id> | --branch <branch> | --change <id>] [--trace]
dockhand cancel [target] [--job <id> | --branch <branch> | --change <id>]
    [--reason <text>] [--wait]
dockhand start
```

`verify <target>` continues the unique open contribution for that port or subport, selecting its prepared committed branch and retaining recorded build settings unless overridden. `--change` and `--branch` disambiguate contributions; an accompanying target must agree. Missing or unprepared contributions cannot fall back to the checkout. `--working-tree` explicitly captures tracked checkout contents, and `--branch` can select committed manual source. Manual selectors may be a name or snapshot-relative port directory/Portfile; names are located through the captured source’s index and validated by native evaluation. Output identifies the source, selected target, and accepted tree. Standalone verification does not create a contribution. Omitting the target infers the single target of the selected tracked branch; multi-target intake remains future work.

The prepared image can be selected explicitly with `--image` or through the Go application's configured default. Otherwise Dockhand selects the conventional image for the native MacPorts platform; `setup` prepares and checks that image. The effective provider settings and image digest are recorded in the job, so queued and admitted work can resume without repeating image-selection flags. The shared pool's capacity is initially two; `--capacity` may establish another positive limit. An existing pool's limit and directory must agree. Omission reuses the recorded limit. Image availability and platform checks are distinct from admission capacity.

At initial planning, the driver looks for reusable evidence in the selected repository. A conclusive pass must cover the same complete tree, target/subport, variants, platform, image digest, verifier implementation, source-build/test policy, provider settings, and artifact inputs. A commit or revision ID can change while the tested tree stays identical. A match settles verification with an original-attempt reference, without consuming VM capacity or creating another attempt. Status and JSON retain that reference and original evidence; `--trace` reports reuse without replaying an old build log.

Standalone Tart `verify` requires a usable prepared image because a reuse miss must be executable; omission selects the conventional native image rather than omitting the provider configuration. An explicitly Tart-selected bump without an available image can record an evidence-selection request instead: Tart, the native platform, and the requested test and source-build policies. Once preparation produces the exact tree, the driver may select the newest matching recorded configuration and reuse its passing evidence. A matching negative result prevents fallback to an older pass. If no pass applies, the prepared branch is preserved and the job directs the user to run setup or select an image. An explicitly selected image that fails setup is never replaced from history. Default automatic bumps instead try GitHub when Tart or a suitable image is unavailable, as described under automatic provider selection below; unavailable GitHub configuration preserves a preparation problem without inventing reusable build inputs.

`verify --fresh` requires a new execution even when a pass applies. This choice is recorded at acceptance and survives detachment. `wait` and `start` continue the recorded choice. A newer terminal attempt with matching inputs and a negative or inconclusive result prevents fallback to an older pass. Lookup checks the latest 32 terminal attempts for the tree and target, so older applicable evidence can conservatively be missed. A miss reports the relevant differences and runs a build. Legacy results without a verifier identity are not reused. Image selection and source binding still run before acceptance; reuse skips driver provider calls and admission, not intake validation. Prepared bumps use this same reuse policy when they reach verification. For a combined publication job, reuse satisfies the admission milestone and leaves publication pending; only confirmation completes the job.

`--from-source` defaults to false for `verify`, `bump`, and `bump-revision`. MacPorts may use available binary archives for the target and its dependencies. Explicit `--from-source` passes MacPorts’ global `-s` option, requiring source builds for ports that need installing; it does not rebuild dependencies already installed in the VM image. The effective choice is recorded at acceptance, so a changed CLI default does not alter existing jobs.

Verification stays in the foreground through completion; `--detach` returns at admission or a conclusive outcome, and `--trace` adds log streaming to stderr. `wait` and `cancel` accept a target, `--job`, `--change`, or `--branch`; omitting selectors uses the current local branch. A branch must identify an open tracked contribution and selects its queued and active jobs at command start. No later job joins that fixed selection. Branch selection reports an error when the contribution has no pending work. `cancel` records intent and runs one cycle; `cancel --wait` continues until settlement. Exact-job cancellation of completed work preserves its existing evidence. `start` advances all eligible work in the selected repository until interrupted, without submitting new jobs or acquiring a singleton driver lock.

Every JSON result is wrapped in one envelope, `{"command", "exit_code", "error", "result"}`, written once after the command runs so the exit code is known; a refused command still writes the envelope with a null result. JSON verification/attachment results contain the selected job ID or branch and frozen job IDs, any acceptance receipt, the last status snapshot, and an interruption indicator. Progress and logs stay on stderr, as JSON lines in JSON mode; see [output](output.md) for the levels. `start --json` writes a stopped/interrupted result when it exits. Exit codes distinguish milestone success (0), failed work (2), needs-attention or superseded work (3), canceled work or process interruption (130), and other errors (1). Confirmed cancellation is success for `cancel --wait`; stopping attachment never submits a cancellation request.

## Preparation assessment

`assess` reports whether Dockhand can prepare an update from committed local HEAD:

```sh
dockhand assess terraform
dockhand assess jq rust-analyzer
dockhand assess rust-analyzer --version 2026-09-14
dockhand assess --maintainer herbygillot@github --category devel
dockhand assess --all --json
```

Explicit ports, maintainer/category filters, and `--all` are separate selection modes. Repeated values within a metadata field are alternatives; maintainer and category fields intersect. `--version` requires exactly one explicit port and accepts the same tag-prefix inference as `bump`.

Every command that selects a checkout, including `status`, `wait`, `gc`, and the bumps, validates it before touching state or fetching anything into it: the working tree, or the checked-out branch when the working tree is sparse, must contain at least one `<category>/<port>/Portfile`. A wrong directory, such as dockhand's own repository, fails immediately, naming the checkout, with a hint to pass `--tree` or set `MACPORTS_TREE`; it is never registered in the state database. Only `setup`, `auth`, and `db` need no ports tree. `dockhand --version` prints the module version or VCS revision embedded at build time.

Default assessment evaluates local declarations and probes literal version inputs without querying upstream. `--version` resolves the requested tag and runs the shared preparation plan through version, source, checksum-association, and edit-fidelity checks, stopping before downloads; human output echoes the resolved candidate version and tag on the port line. Neither mode opens workflow state, creates jobs, changes branches, executes dependency generators, or builds ports. It evaluates Tcl in an isolated materialization; this is not a sandbox for untrusted Portfiles. Working-tree edits are excluded. Indexed scans may create or refresh the source-bound index cache.

Each result retains findings with a check, status, stable reason code, and explanation, plus input locations where available:

- `input-found` (shown as "ready"): local checks found literal candidates. A specific update has not been established; candidates can still be ambiguous or unsuitable for the requested release.
- `candidate-checked` (shown as "candidate ready"): a resolved release passes the pre-download preparation checks.
- `blocked`: a required optional helper, such as `cargo2port` or `go2port`, is unavailable.
- `unsupported`: an established preparation limitation, including unsupported source conventions or edits that change unrelated metadata.
- `unknown`: evaluation, probing, upstream observation, or index coverage could not establish an answer.

The codes are the JSON values; human output prints the plain words in parentheses. All findings remain visible when several conditions apply; the overall result prioritizes unsupported, then blocked, then unknown. Archives, generated-manifest equivalence, and verification remain explicitly untested. Successful assessment does not promise that a download, full preparation, or build will succeed. Use `bump --diff` to exercise full source preparation.

Independent ports continue after a per-port failure. Indexed subports are selected within their owning Portfile; explicit selection uses `assess <name>`. Unresolvable targets are reported individually; omitted Portfiles and missing index entries remain unknown. Exit status is 0 when all results are `input-found` or `candidate-checked` (or no ports match), and 1 if any are blocked, unsupported, or unknown. Cancellation follows the normal CLI exit convention.

## Implemented preparation groundwork

```text
dockhand bump-revision <port> --diff
    [--variant +name|--variant=-name ...]
    [--reason <text>] [--json]
dockhand bump <port> [version]
```

New version and revision bumps, including previews, fetch `master` directly from `https://github.com/macports/macports-ports.git`, independent of local remote names. They freeze its commit before evaluation and acceptance. Fetch failure stops the request; there is no stale fallback or local starting-branch option. Existing contributions use `verify <target>` and `publish <target>`; explicit branches remain available. Local branches, remote-tracking refs, FETCH_HEAD, the checkout, and index are preserved. The accepted source stays fixed across retries and later upstream movement. It materializes the complete tree, evaluates the original Portfile and all its subports, proposes a focused revision edit, and evaluates the candidate tree. A selected subport can change without its siblings changing. A shared revision edit that changes unselected siblings, changes other evaluated metadata, or fails evaluation is refused. Revision expressions and ambiguous or dynamically named scopes remain unsupported by this first editor; versions may still be calculated because MacPorts evaluates them.

Successful previews render a Git diff to stdout and source/target information to stderr. JSON returns the selected branch, preparation result and evaluations, commit intent, and diff. Preview writes immutable Git objects as needed, but creates no branch, commit, job, database, or verification environment and leaves the user's checkout/index alone. The driver uses this same preparation service for accepted version- and revision-bump jobs.

### Version bumps

```sh
dockhand bump jq --diff
dockhand bump jq                           # verify, then open the PR; reuse applicable evidence if present
dockhand bump jq --no-publish              # stop after verification
dockhand bump jq --image dockhand-base-tahoe
dockhand bump jq 1.8.1 --diff
dockhand bump jq jq-1.8.1 --no-verify
dockhand bump jq 1.8.1 --image dockhand-base-tahoe --detach
```

For GitHub/GitLab sources, an explicit version or tag is resolved against the evaluated repository. Other archive sources accept an explicit evaluated version without inventing a forge tag or Git commit. Supported native HTTP regex livechecks also supply automatic candidates. The PortGroup prefix and suffix supply the inferred tag; no generic `v` is removed or added independently of that convention. Missing, ambiguous, and failed lookups are distinct errors. Explicit selection can choose an older version; an unchanged version is refused. Omitting the version requests automatic selection. Standalone `refresh-checksums` is implemented and preserves the version and revision. An explicit version is honored even when it is a prerelease: `bump`, `assess`, and `status` say that the change takes the port out of stable, and the machine never refuses a version a person typed.

Automatic selection requires a stable or prerelease numeric current version and a supported evaluated regex `livecheck` convention. A port on a stable version selects stable releases only; a port already on a prerelease, as `-devel` ports are, follows prereleases as well, still through its own livecheck filter, with `livecheck.version` equal to the port version. Forge discovery also requires the matching repository URL. An unset `github.tarball_from`, `archive`, and `tarball` select GitHub repository tags without consulting GitHub Releases. Only `github.tarball_from releases` selects published GitHub Releases, excluding drafts and marked prereleases. GitLab selects repository tags. Release metadata does not veto a tag in tag mode. Every mode excludes tag versions containing anything besides digits and dots. Explicit versions remain available for prereleases and other spellings.

Dockhand applies the evaluated Tcl regex to candidate text matching the PortGroup input: archive URLs for GitHub and Atom tag-entry IDs for GitLab. It orders matching versions with MacPorts `vercmp`. This respects supported version-line filters. For non-forge HTTP regex sources, Dockhand downloads the declared listing and extracts captures using native Tcl line-oriented matching. Custom livecheck scripts and hooks remain unsupported. Supported request headers and compression are honored; other request options produce an explicit refusal. A complete catalog and one unambiguous newest eligible tag are required. HTTP failures, incomplete pagination, no matches, and ambiguous candidates produce unknown/needs-attention, never an already-current result. Catalogs use their forge SDK iterators with default options and follow pagination to completion. Dockhand adds no catalog item/page ceilings, page-size overrides, response-size limits, or adapter/discovery timeouts. Caller cancellation and workflow call deadlines still apply. HTTP listings use the shared bounded transport with a 16 MiB response limit; source URL, response identity, and selected version are retained without storing the page body.

When the newest eligible version is equal to or older than the accepted source version, the job completes successfully with an already-current detail and a recorded release observation. It closes the initial contribution intent without creating a branch or verification attempt; an unavailable image does not prevent this no-op. A preview emits no diff and opens no database. `wait` reattaches to the completed job without rediscovering versions. This records the result of that invocation, not continuing proof that upstream is unchanged. See the [automatic-selection report](activity/2026-09-13-automatic-version-selection.md).

The editor probes source literals through native MacPorts evaluation. It supports literal, component, separator, and numeric transformations when one actual candidate establishes the requested source reference and evaluated version. Ambiguous inputs remain refused. Subports are selected by their exact name as the positional target; existing Terraform/Helm series can be updated without altering their siblings or obsolete parent. Creating a new series is a separate operation.

Archive preparation observes exact checksum/revision declarations and uses MacPorts' fetch planner for filenames, tags, mirror aliases, and assembled locations. Nested and append/prepend checksum declarations retain their source ownership. The planner covers relevant architectures and literal Darwin boundaries, downloads affected HTTP(S) archives, and applies all checksum edits together. Unchanged filenames do not imply unchanged artifacts. Independent old-version and auxiliary-source pins remain protected. The candidate is evaluated again without tracing in every covered context, followed by native validation of the stored Git tree.

Modeled metadata is explicitly separated from native runtime facts and build verification. Unresolved context boundaries, unassociated checksum groups, host-dependent modeled evaluation, changed declaration ownership, and conflicting required digests remain gaps or refusals. Fetch credentials, unrecognized hooks, remote patches, and unsupported dependency forms retain their existing restrictions. Supported dependency generators still regenerate manifest-backed blocks separately. See [bump preparation](bump-planner.md) for implementation limits and exercises.

The driver records the resolved release before archive preparation. Forge tags are checked before and after preparation; a moved or missing tag requires attention. HTTP-listing candidates retain their observation and selected version across retries without reselecting from a newer page. Previews report the release and diff without opening state; normal jobs create `dockhand/bump/<port>-<job suffix>` through the same integration machinery as revision bumps. `--no-verify`, `--no-publish`, `--image`, `--capacity`, `--tests`, `--from-source`, `--detach`, `--trace`, `--reason`, `wait`, and `start` have the same meanings on both bump paths. A verified bump continues through the shared publication phase unless `--no-publish` was given. See the [implementation report](activity/2026-09-13-explicit-version-bumps.md).

### Revision-bump jobs

```sh
dockhand bump-revision jq --no-verify
dockhand bump-revision jq --image dockhand-base-tahoe
dockhand bump-revision jq --image dockhand-base-tahoe --no-publish --detach
```

A new contribution starts from freshly fetched authoritative MacPorts `master`, using the same immutable intake as version bumps. Equivalent repeated requests join existing work; retries retain the contribution and its original source. A new contribution receives a branch named `dockhand/revbump/<port>-<job suffix>`. The source branch, checkout, and index stay in place. Git author identity, source commit/tree/base, target, platform, and any verification configuration are captured before acceptance. `--reason` becomes the commit body, followed by the generated-contribution trailer. The output reports the job, prepared branch, commit, and result revision.

`--no-verify` completes once the branch is recorded. Otherwise the command prepares the branch and stays through verification and, unless `--no-publish`, publication; `--detach` returns at verification admission or evidence reuse and `--trace` also streams logs. Revision bumps accept the same image, capacity, test, and source-build options as `verify`. Automatic bumps bind one concrete provider at intake when available; applicable evidence can satisfy its verification after the prepared tree is known. Explicit Tart requests may also use the evidence-selection behavior described above. A missing executable configuration or reuse miss in that evidence-only path is recorded as needs-attention after preserving the branch. A later `verify <target> --image <image>` continues the contribution with a new verification job. The same job continues through publication of the verified prepared revision by default, using the destination options of standalone `publish`; `--no-publish` stops before it. Publication requires verification, so `--no-verify` implies `--no-publish`.

`wait --job <job_id>` and `start` resume recorded preparation and integration as well as verification. Cancellation before integration leaves no output branch. After integration may have started, the driver reconciles the recorded candidate and preserves any confirmed branch. An interrupted integration whose branch is absent or contains another commit requires attention; it never overwrites user work or guesses that a deleted branch should be recreated. See the [driver implementation report](activity/2026-09-13-revision-driver.md).

## Approved source selection and human edits

Working-tree capture, standalone verification, single-target inference from a tracked contribution, and branch-selected waiting and cancellation are implemented. Broader target selectors remain future work.

When the port is omitted, the selected branch must belong to an open contribution with one recorded target. Named continuation and inference reuse that target's Portfile, evaluated name, internal subport identity, and variants. Explicit `--variant` choices override matching recorded choices while preserving the rest. An explicit manual source starts from its evaluated target and supplied flags.

Inference compares the whole selected source tree with the recorded contribution base. Every changed path must stay within that port directory; another port, shared resources, or unrelated repository edits require an explicit port. Rename detection is disabled so both sides of a move count. An unavailable base or changed evaluated target identity also requires explicit selection. This conservative first rule can require an explicit port after a rebase introduces upstream changes elsewhere. Untracked branches, detached checkouts, and zero/multiple recorded targets never guess from HEAD or the latest job.

The CLI identifies inferred selection and displays effective variants before acceptance. Binding freezes the source and resolved target; acceptance rechecks contribution/revision identity and the recorded target used for inference. If another process changes the target without changing its revision, acceptance still fails rather than silently adopting a different scope. The driver receives an explicit target and uses ordinary verification/reuse.

The initiating target is the everyday selector for a tracked contribution; its durable identity is the change ID, and job IDs identify exact executions. Users can edit, commit, and rebase with ordinary Git commands, then ask Dockhand to verify or publish without a separate adoption step for every edit. A renamed or missing tracked branch requires an actionable error rather than silent reassociation.

| Command | Source and target selection |
| --- | --- |
| `bump <port\|selector> [version]` | Prepare the requested update; omission of the version requests the latest eligible release. Explicit version resolution is specified below. |
| `verify <target>` | Verify the unique open contribution's committed branch and inherit its recorded build settings. |
| `verify` | Use the current tracked branch and infer its single target. |
| `verify <target> --working-tree` | Explicitly capture tracked checkout contents, including staged additions. |
| `verify [<target>] --branch <branch>` | Select committed manual source or a matching tracked contribution. |
| `publish [<target>] [--branch <branch>]` | Publish the tracked target; without a selector, use the current branch. |
| `wait [<target>]` | Freeze and follow pending work for the contribution; also supports --job, --branch, or --change. |
| `cancel [<target>]` | Record cancellation for the same fixed selection. |

Port names, branches, contribution IDs, and job IDs are explicit selectors, not guesses based on token spelling. An accompanying target must agree with its contribution selector. Empty selectors are rejected. Bare wait/cancel use the current tracked branch; detached HEAD requires a selector. Later submissions never join an existing wait/cancel scope. A missing or unprepared contribution never falls back to verifying the checkout.

Explicit `--working-tree` verification captures the working-tree contents of tracked files, including deletions and staged additions, without altering the user's index or making a commit on their branch. Where a staged file has further unstaged edits, its working-tree contents are selected. Initially, new files must be staged to be included; report relevant untracked files with instructions to stage them rather than silently omitting a required patch. Explicit `--branch`, including the current branch's name, selects only committed contents. Capture includes raw working bytes, symlink targets, and executable modes without running Git clean/smudge filters or rewriting the real index. Nonignored untracked files under the selected port, any modified port directory, or shared `_resources` require staging before submission. Other untracked files are excluded. Sparse/skip-worktree entries, unresolved conflicts, submodules, unsupported file types, and individual files larger than 128 MiB are refused by this initial capture path; committed `--branch` verification remains available where its existing materializer supports the source.

Capture reads tracked contents twice and rechecks index entries and HEAD. A detected change requires a retry. This catches ordinary concurrent editing but is not an atomic filesystem snapshot. Once accepted, the Git tree is immutable; the driver never recaptures the checkout. Dirty inputs have no source commit, and the observed HEAD is recorded separately as provenance. Clean inputs retain their matching commit. No synthetic commit or hidden ref is created.

Before expensive work, display the branch or detached source, whether input comes from the working tree or a commit, the number of modified files captured, and the verification targets. Freeze the accepted snapshot before submission. Subsequent edits, commits, or branch movement cannot change a queued or running build. Reattaching to its job continues the original accepted work.

Verification evidence describes the tested source tree and build inputs. Committing an unchanged tested tree can preserve applicable evidence even though the commit ID changes. Publication selects committed source and checks the full tree, target/configuration coverage, environment, and other build inputs; matching just a Portfile, version, or commit message is insufficient. If the tested edits remain uncommitted, tell the user to commit them before publishing. Report when committed contents differ from the tested snapshot and need verification.

A contribution may change several ports. The edited-port set and the verification-target set are distinct: dependents can need testing without edits, and edits can alter the required coverage. Remember the user's intent and reconcile later source changes with it. Additional unrelated ports or shared PortGroup edits can require an explicit scope decision rather than silent inclusion or exclusion.

Standalone verification does not establish an exclusive contribution association for its branch. Running `verify jq` and then `verify terraform` on `master` must work independently. Both jobs retain exact source and target identities without turning `master` into a one-port contribution.

## The flow

The standard workflow for `dockhand` involves bumping a port's version to its latest release by default, bumping its revision, or refreshing its checksums. This produces a Git branch with the proposed changes. Build verification of these changes is requested by default unless `-N` / `--no-verify` is specified, and a verified branch is submitted as a pull request against [macports/macports-ports](https://github.com/macports/macports-ports) unless `--no-publish` is specified.

The driver owns each accepted job and its bookkeeping. The CLI submits the request transactionally to the state store through the shared workflow API, runs targeted driver cycles in the same invocation, and observes recorded progress. A normal invocation remains attached until the requested work completes, running the required cycles itself; `--detach` returns once the verification provider accepts the build. After a detached invocation exits, further workflow advancement requires `wait`, a running `dockhand start` process, or a later driver cycle. Both modes use the same workflow implementation; commands do not launch background drivers.

```text
dockhand bump <port|selector> [version] [-N|--no-verify] [--no-publish] [--detach|--trace]
dockhand (bump-revision | refresh-checksums) <port|selector> [-N|--no-verify] [--no-publish] [--detach|--trace]
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

# Prepare, verify, and publish a PR, staying attached throughout.
dockhand bump jq

# Verify only; stop before publication.
dockhand bump jq --no-publish

# Submit and return after build admission. Publication then requires
# wait, a running persistent driver, or a later driver cycle.
dockhand bump jq --detach

# Create only the branch, with verification explicitly skipped.
dockhand bump jq --no-verify

# Publish the current contribution, or select a branch.
dockhand publish [--branch <branch>] [--detach]

# Verify current-checkout edits; omit targets when contribution scope is clear.
dockhand verify [<port|selector>] [--detach|--trace]

# Explicit branch selection verifies committed contents.
dockhand verify [<port|selector>] --branch <branch> [--detach|--trace]

# Read all recorded work, one exact job, or a contribution branch.
# --active narrows any view to queued and active jobs.
dockhand status [<job_id>] [--active]
dockhand status --branch <branch> [--active]

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

When the port declares patch files, preparation checks each one against the downloaded candidate source with the system patch command in check mode and reports the result. `bump --diff` prints one line per patch. A rejected patch is a finding, not a refusal: `bump` still creates the branch, but the job finishes as needs-attention naming the patch instead of starting a build that would stop in the patch phase; refresh the patch on the branch, then verify. Patches the check cannot model, such as xz-compressed files or unrecognized `patch.pre_args`, are reported as unchecked.

The version argument expresses the requested release; it need not be the literal text eventually written to the Portfile's `version` field. Keep the user's input, the resolved MacPorts version, and the upstream tag/reference distinct. For example, if the port currently derives tags as `v${version}`, input `1.8.1` can resolve to tag `v1.8.1` while the Portfile version remains `1.8.1`. These examples illustrate the command syntax, not a claim about jq's actual upstream tag spelling.

Preserve a prefix the user explicitly supplies. If the prefix is omitted, use the current Portfile's evaluated upstream reference and its version-to-tag convention to infer the candidate; do not hardcode `v` or derive the convention from the version field alone. Confirm the candidate against upstream release/tag evidence before applying edits. Do not double a prefix, strip arbitrary text, silently substitute another release, or treat failed upstream lookup as proof that a tag does not exist. If the convention is unclear, multiple candidates match, or no candidate can be confirmed, report the candidates or lookup failure and ask for an exact reference where that resolves the problem.

Display the resolved version and tag when they differ, including in `--diff` output. Ports using release archives without Git tags resolve the requested version through their source convention without inventing a tag. Automatic latest-release selection and explicit-version resolution share this preparation path. Once resolved for accepted work, record the chosen source identity so retries do not rediscover a different release.

**Return behavior and capacity**

| Request | When the CLI returns |
| --- | --- |
| Verification and publication (the default) | After publication completes or verification/publication requires attention. |
| Verification with `--no-publish` | After the requested verification finishes or requires attention. |
| Verification when the provider is at capacity | It reports that it is waiting, stays attached, and submits when capacity becomes available. |
| Any of the above with `--detach` | After the provider admits the initial build, or an earlier conclusive result is available; later settlement and publication require `wait` or a persistent driver. |
| `--no-verify` | After the driver records completion of branch creation. There is no provider-admission milestone, and no publication, since it requires passing verification. |
| Default publication that cannot be bound | Rejected before submission, before any preparation: no GitHub login, no fork, or an ambiguous remote layout; the refusal names `--no-publish`. |

For an existing change, standalone `publish` requires matching passing evidence and stays through confirmation by default. Scheduling missing verification or joining an active build from this command remains later work. A combined bump performs its own verification or reuses matching evidence before publication.

For a selector, the return condition applies to each selected target operation. With `--detach`, each must reach its applicable handoff milestone or report a conclusive result. When there are more initial builds than available slots, this can require waiting for earlier builds to finish. The driver continues independent targets when another fails. Downstream follow-up builds remain the driver's responsibility after the initial admission; the default attachment follows the full requested operation.

**Waiting, tracing, and cancellation**

Requests and progress pass through the state store. Attachment means observing the selected durable jobs, not opening a socket to a driver. Successful submission records a durable job; the command then stays attached through completion, or with `--detach` only until provider admission. After a detached invocation exits, report pending work and how to resume it without promising automatic advancement when no persistent driver is running.

`--detach` changes attachment, not the requested destination. The destination is chosen by `--no-verify` and `--no-publish`; with verification skipped, attachment ends at branch creation.

`--trace` keeps the default completion behavior and also streams build logs. It is available for a single selected port when verification is enabled. With publication, it stays attached through the publication result after the build logs end. Reject incompatible requests such as `--trace --no-verify` or `--trace --detach`, or tracing a multi-port selector, with a clear usage error.

`wait` attaches to existing work; it does not submit a fresh verification or request publication. It can run targeted cycles in the current process to resume the selected durable jobs, sharing the same engine and state claims as `dockhand start`. It binds the jobs and revisions selected when the command starts rather than silently following future requests or new branch tips.

Once a job is durably accepted in the database, Ctrl-C stops the CLI's observation, including before driver pickup or while waiting for provider capacity. Accepted work remains in the database, and submitted provider builds may continue. Further workflow advancement requires a running persistent driver or another driver cycle. Use `cancel` to ask the driver to stop outstanding verification and any pending publication continuation. Cancellation preserves the branch and completed evidence; it does not undo an already-published PR. Cancellation and resource cleanup are recorded and performed by the driver.

**Unavailable verification and explicit skipping**

Temporary capacity pressure is different from missing tools, an unprovisioned environment, or an unsupported target configuration. Capacity pressure waits for admission. A setup requirement or a preflight refusal returns an explicit result and a next action instead of waiting indefinitely for a slot that cannot become usable.

Branch creation remains useful when verification tools are unavailable. Preserve the branch and record why verification could not proceed. Distinguish that from `--no-verify`, where the user explicitly chose to skip verification. Neither case is a verification pass.

Publication requires passing verification. If verification is unavailable, the job records the setup requirement, preserves the prepared branch, and ends needing attention, so the default bump exits 3 rather than quietly succeeding without a PR. `--no-verify` requests branch creation on its own and therefore implies `--no-publish`. A negative build result is never a pass.

**Observation, previews, and results**

`status` reads driver-maintained state. It shows each target's revision, verification state, publication state, status of its associated pull request, and any blocker or setup requirement. Outstanding resource cleanup remains visible separately from the job outcome. Include the last observation time so stale information is visible. It does not take over bookkeeping when no driver is running. PR monitoring and forge-state refresh belong to the driver.

`status` is contribution-centric. The engine projects its snapshot into one row per contribution (`workflow.Project`), or per standalone verification: the port, the version move or kind of change, the phase (preparation, verification, publication, done), the phase's current word (preparing, building on macOS 26, waiting for capacity, verified, published, merged, needs attention), what comes next ("verification pending", "PR open, 3 checks pending", "merged; branches cleaned", "patches no longer apply (2); refresh them and amend"), the PR, the one active job, and the job history. Plain `status` prints that table, and on a terminal it is live (`internal/tui`) and a processor: the driver loop runs beside the table for as long as it is open, with the header marked `(processing)` and its reports in the message strip, so a person watching work is also advancing it and detached jobs never stall for lack of a driver. The projection is polled every two seconds, a selected row expands to its history and identifiers, and keys map onto the existing verbs by running them in-process on the selected contribution (`--change <id>`, or `--job` for standalone work), so a key has exactly the authority of the command; bump, verify, and publish run with `--detach` and the table's processing carries them, and bump, verify, publish, cancel, and abandon confirm first. `--print` prints the snapshot once without processing; `--json` and non-terminal output imply it, and those three paths remain the read-only status. A preparation that stopped before creating a branch, such as one refused by an expired GitHub credential, says "bump again once fixed", and the `b` key does exactly that. `--json` returns the snapshot with the projection beside it as `Contributions`; `-v` prints the full record with identifiers and times. The derivation of these words lives with the projection, so the table, the JSON, and the live view cannot disagree.

`status` selects the current repository within the chosen database and reads a consistent view of its jobs and related records. Without filters it includes all recorded work and outstanding cleanup, including cleanup after jobs finish. `status <target>` selects its recorded contribution history; `--job <job_id>` selects one exact job and `--change <id>` selects one contribution. `--branch <branch>` selects jobs linked to contributions with that recorded branch name, including closed contributions and branches whose Git ref no longer exists. It does not match the source branch from which a bump began or standalone verification's checkout provenance. Job ID and branch selection are mutually exclusive.

`--active` can combine with either selector and includes queued or active jobs, including capacity waits and future retries. Terminal failures and needs-attention jobs are excluded; their retained resources remain visible in unfiltered status or a view selecting those jobs. Filtered views include only the selected jobs' contributions, input/result/current revisions, PRs, and resources. Reused verification cites its original attempt without including that original job's resources. Selection happens in SQLite before decoding job details; related reads use bounded batches in the same snapshot.

Snapshot-read time is separate from evidence and PR observation times. Human output includes each job's recorded workflow phase and escapes embedded control characters. JSON uses the typed `workflow.Status` projection, with empty collections represented as arrays and an optional `Filter` describing the requested selection. Missing database or repository registration produces empty status; an explicit unknown or foreign-repository job ID returns not-found, even with `--active`. A known terminal job with `--active` produces an empty matching set. Unreadable, corrupt, or unsupported state is an error. Status does not initialize or migrate the database, register repositories, or mutate workflow records.

`--diff` performs only the preparation needed to show the proposed changes. It may evaluate Portfiles and fetch inputs needed to calculate checksums, but it does not edit the working tree, create a branch, persist a job, start a build, or publish a PR. Reject combinations with `--no-publish`, `--detach`, or `--trace` that ask a preview to execute the workflow.

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

`rebase` and `amend` follow the same `--no-publish` and `--detach` conventions. `dockhand amend --branch <branch>` incorporates selected corrections, verifies the resulting revision, and updates the existing PR; `--no-publish` stops after verification, which never publishes local corrections by itself. Neither command silently includes unrelated working-tree edits.

`status` distinguishes the local revision, evidence applicable to it, the last confirmed published revision, and the latest recorded PR observations. In phase two, `start` refreshes PR state, remote CI checks, review decisions, and conflict information. Before a field has been observed, it is unknown; an old observation is shown with its age. Observations do not automatically trigger corrective work.

Attachment and `wait` follow the selected job's requested destination. Publishing still completes when the PR is opened or updated, even when the change remains under review. They do not become indefinite waits for PR approval or merge. Later rebase, correction, verification, and publication requests create new jobs associated with the same tracked change.

## Implemented standalone publication (2026-09-13)

`publish [--branch <branch>] [--remote <remote>] [--upstream <remote>] [--base <branch>] [--dry-run] [--detach]` is now connected to the shared driver. It takes no port argument. Omitted `--branch` uses the current branch's committed contents, even if the checkout contains uncommitted edits. The command shows its bound commit and evidence. `--dry-run` performs local/remote preflight and renders the plan; it accepts no job and performs no remote write. The preflight includes the authenticated-user and push-repository ownership checks, so a dry run needs the same GitHub credential as publication. It reads verification state through normal DB service initialization.

The initial executable scope is one contribution commit in one port directory, with passing evidence already recorded for its complete tree/target and selected configuration. Standalone publication tells the user to verify first when evidence is missing or not passing. A combined bump uses the shared preparation and verification path before publication. For a user-created branch, the changed port directory selects the latest terminal verification for the exact tree. Its recorded target, subport, variants, and configuration appear in the plan and status. Publishing adopts that branch in the same transaction as the publication request. Neither verification nor `--dry-run` adopts it. Root commits, merges, multiple unpublished commits, empty changes, and changes outside one port directory are refused.

The default push remote is `origin`. PR target discovery prefers a configured `upstream` remote, otherwise the push repository's fork parent, otherwise the push repository. The target's default branch supplies the base unless overridden. The push repository must be owned by the authenticated GitHub login; a remote that resolves to `macports/macports-ports` or to another contributor's fork is refused before acceptance, and the refusal lists the local remotes whose repository the login owns. The same ownership check runs again when a frozen destination is planned for publication. The commit supplies the PR title and contribution description. The initial body adds Dockhand attribution, recorded guest environment details, and an evidence-based checklist; the full body appears in `--dry-run`. Lint, tests, and install are checked only when the selected attempt records successful execution for the target. Test omission, actual argv/user, and missing historical metadata are explicit. The single-commit check additionally requires an unchanged recorded generated commit. Other review checks remain manual. An existing PR's body remains intact. The API uses `GH_TOKEN`, `GITHUB_TOKEN`, Dockhand's Keychain credential, or the active `gh` login, while Git authentication stays with Git.

The command stays through confirmation of the pushed head and PR metadata; `--detach` runs a driver cycle and returns after pickup or an earlier conclusive outcome. `wait --job <job_id>`, `start`, cancellation, JSON output, and detachment use the existing workflow path. An uncertain issued PR request is observed without another write; it can remain pending when the remote outcome cannot be established. `status` shows that state and the retained PR URL after confirmation.

## Implemented combined bump and publication

```sh
dockhand bump jq [version] [--image dockhand-base-tahoe] [--detach|--trace]
dockhand bump-revision jq [--image dockhand-base-tahoe] [--detach|--trace]
```

Both accept `--remote`, `--upstream`, and `--base` with the same defaults as `publish`: the fork is the remote pushing to a repository the authenticated login owns, the upstream is the remote whose URL names `macports/macports-ports` whatever its name, and `--remote` is required only when that is ambiguous. On bump commands those flags need publication, which is the default; they are refused with `--no-publish` or `--no-verify` unless GitHub verification uses them. Destination repositories, URLs, base branch, and the operation-lock directory are frozen before acceptance. No remote ref or PR is written during intake.

One job owns preparation, verification, and publication. Its accepted input source remains the original commit; its result revision identifies the prepared commit that is built and published. A passing build or applicable reuse leaves the job active for publication. When `--image` is omitted, the exact configuration comes from the selected evidence while accepted provider, platform, test, and source-build requirements remain fixed. The driver then records remote preconditions and content before any push, using the same executor and uncertainty handling as standalone publication. Status shows the requested destination before that publication checkpoint exists.

By default the command stays attached until PR confirmation. With `--detach` it waits through capacity pressure and returns at build admission, evidence reuse, or an earlier terminal outcome; `wait <port>` and `start` resume the exact accepted destination without repeating flags. An automatic no-update result needs no branch, verification, or PR.

Failed verification prevents publication. A changed or missing prepared branch or newer matching negative evidence stops fresh remote effects; human edits require a new explicit verification/publication request. Cancellation before a PR request preserves any branch already created or pushed. Once the PR request has started, the existing observation-only recovery applies. This slice adds no unverified publication override, automatic rebase/squash, downstream scheduling, or post-PR monitoring.

### Calculated source versions

Upstream tags and evaluated MacPorts versions can differ. Dockhand discovers literal inputs, tests candidate edits with the native evaluator, and preserves the Portfile's Tcl calculations. For example, a source tag `2026-09-14` can evaluate to port version `20260914` without a calendar-specific rule. Automatic selection applies the livecheck filter to upstream source spelling and compares the evaluated versions with MacPorts `vercmp`.

Specify an upstream tag or its source version with the usual optional prefix. Dockhand does not infer an arbitrary inverse from a calculated port version: use `2026-09-14`, rather than expecting `20260914` to reconstruct that tag. Ambiguous edits, inconclusive candidate evaluation, changed siblings, and unrelated metadata changes stop the selected update. Explicit selection may request an older version; automatic selection advances only when the newest eligible evaluated version is newer than the current one.

## GitHub verification provider

Build commands accept `--provider tart|github`; bumps additionally accept `auto` and use it by default, preferring a suitable prepared Tart image. Standalone `verify` defaults to Tart. `github` pushes a committed contribution to a personal `macports-ports` fork and observes its existing `main.yml` workflow. `--remote` selects the fork's Git remote, defaulting to `origin`. `bump --provider github` continues to publication after workflow success. Explicit `verify` requires `--branch`.

GitHub selects `--tests workflow` by default, reflecting the workflow's tolerance of individual test failures. It refuses Tart image/capacity/source-build flags and variant overrides. A newly accepted GitHub verification observes the current remote run attempt rather than reusing old local evidence; `--fresh` does not dispatch a rerun. `--trace` retrieves completed-job logs. See [GitHub verification](github-verification.md) for coverage, credentials, recovery, and initial limits.


### Retry and credential behavior

Waiting is scheduled by what is waited for. Capacity, running builds, and cancellation acknowledgement are unbounded: capacity polls at the wait interval, which defaults to ten seconds, builds at the observe interval, and cancellation at the retry delay. Reconciliation of an unconfirmed submission, forge reflection of a pushed branch or pull-request write, and confirmation of a resource release are budgeted: their interval doubles up to five minutes, and after twenty consecutive waits without progress the attempt is recorded as errored, the publication job as needing attention with an uncertain write left uncertain, or the release obligation is kept but polled daily. Consecutive waits are stored with the record together with the kind they were for, so a wait of another kind starts the count over; status shows the count and the kind. Failures use a separate exponential schedule starting at one second, with job-specific jitter and a five-minute ceiling; explicit engine timing overrides are available for embedding and tests. Server rate-limit deadlines take precedence over that ceiling. Consecutive failure counts and deadlines are stored with each job, attempt or resource, so another driver observes the same schedule. Successful operations reset the failure count and expected waiting resets it too; a failure or a settled result resets the wait count. Cancellation can bypass verification waiting deadlines. Missing providers remain recoverable by a correctly configured driver and are named explicitly; a configured provider registry never falls back to a different provider.

A confirmed GitHub rate-limit refusal may retry a publication write after the cooldown. The transaction records the refusal and clears only that outstanding write intent; an interrupted response or transport failure keeps the existing observation-only recovery rule. If the refusal itself cannot be recorded, recovery remains conservative. Schema 14 adds retry counters and publication refusal counts without changing accepted intent or existing evidence. Writable opens migrate supported older databases automatically; `dockhand db upgrade` performs the upgrade explicitly. Read-only status does not migrate.

Preview and execution use the same credential setup. Public GitHub reads use an available credential without requiring a prior authenticated operation. Only absence of credentials permits anonymous reads; inaccessible, malformed or rejected credentials are reported, without silently trying anonymous access or another credential. An explicitly configured alternative API endpoint does not receive implicit system credentials.

Provider-specific admission failures are classified by meaning rather than forced to occur at the same point. Invalid GitHub policy or fork configuration fails during intake. A missing usable Tart image may preserve a prepared branch and report the remaining verification requirement. `--no-publish` selects the destination, while `--detach` selects attachment only through admission; detached output names pending PR publication explicitly.


### Automatic bump verification

`bump` and `bump-revision` default to `--provider auto`. Intake prefers the conventional prepared Tart image for the native platform (the Xcode profile when the port requires it). Missing Tart or an unavailable suitable image selects GitHub, using the normal personal-fork authentication and workflow requirements. When Tart is installed but the image is unavailable, output suggests `dockhand setup`, including `--xcode` for an Xcode requirement. Capacity is decided later by the selected provider; a full Tart queue waits rather than switching to GitHub. Local inspection errors, cancellation, and missing unrelated tools do not trigger fallback.

Explicit provider choices remain authoritative. Image, capacity, variant, source and local test-policy options select Tart; `--tests workflow` selects GitHub. Default tests are chosen after the provider: declared tests for Tart, workflow policy for GitHub. The accepted job retains a concrete provider; no second verifier is submitted after a Tart run. The repository may independently trigger Actions when publication pushes the branch or opens the PR. Standalone `verify` keeps its Tart default.

An automatic GitHub configuration failure is retained as a preparation verification problem, without inventing a build configuration or borrowing unrelated evidence. This preserves no-op bumps and useful prepared branches. Explicit GitHub errors still stop intake.

### Shared-release authorization

For a named subport whose release is shared with siblings, inspect with `dockhand assess py313-ipdb --version <version> --shared-release` or preview with `dockhand bump py313-ipdb <version> --shared-release --diff`. Use `bump --shared-release` to authorize the listed release cohort. Continue with `verify py313-ipdb` and `publish py313-ipdb`; the immutable revision retains the required sibling coverage. Each buildable sibling needs passing local verification. Metadata-only parents are recorded but do not receive a build. Multi-target GitHub verification is not yet supported.
