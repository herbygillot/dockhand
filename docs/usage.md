# Using Dockhand

Detailed command behavior and operational choices for the current prerelease. For database backups, restoration, and resource retention, see [state operations](operations.md).

## Tools and checkout selection

Select a ports checkout with `--tree` / `-t` or `MACPORTS_TREE`; otherwise Dockhand uses the current directory. Whichever it is, every command that works on a checkout, including `status`, `wait`, `gc`, and the bumps, first checks that it is a ports tree, with at least one `<category>/<port>/Portfile` in the working tree or on the checked-out branch, and refuses anything else, such as dockhand's own repository, before touching the state database or fetching into it. Only `setup`, `auth`, and `db` need no ports tree. Select the local MacPorts installation with `--prefix` / `-p` or `MACPORTS_PREFIX`; otherwise it finds `port-tclsh` on `PATH`. That installation evaluates ports on the host; the verification image always builds with MacPorts at `/opt/local` and the Base release `setup` installed, independent of the host prefix. When the two Base releases differ, `setup` says so when it provisions or checks the image, and a bump or verify says so when it selects an image whose MacPorts has been observed; the work proceeds, since neither release is wrong on its own. Select Git with `--git` or `GIT_BIN`, and Tart with `--tart` or `TART_BIN`; otherwise Dockhand finds each executable on `PATH`. `DOCKHAND_INDEX_CACHE` relocates the shared PortIndex cache, which otherwise lives under the user cache directory as `dockhand/indexes`. Flags override the environment. `--no-publish` has no shorthand.

## GitHub authentication

Authorize Dockhand for GitHub publication without installing `gh`:

```sh
dockhand auth login
```

Dockhand includes the public client ID for its registered OAuth application. Login opens GitHub's device page, requests `public_repo`, validates the selected account, and stores the token in macOS Keychain. `--client-id` or `DOCKHAND_GITHUB_CLIENT_ID` overrides the registered application for development, and `--no-browser` prints the URL for manual opening. No client secret is present or required. Login uses neither the ports tree nor the workflow database.

An existing authenticated GitHub CLI login, `GH_TOKEN`, or `GITHUB_TOKEN` also works. Git push authentication is configured separately through Git. The publication section below describes credential precedence and remote selection.

`dockhand auth status` checks the selected credential against GitHub and reports its source and account. Rejections name the source and how to replace it; Dockhand does not try another identity after rejection. `dockhand auth login` replaces the saved Keychain credential. If `GH_TOKEN` or `GITHUB_TOKEN` is set, login explains that the environment credential still takes precedence.

`dockhand auth logout` removes only Dockhand's Keychain entry; an absent entry is already logged out. It does not revoke the token on GitHub or modify environment variables or `gh` credentials. A later invocation may therefore select an environment token or the GitHub CLI login. Run `dockhand auth status` to check. Both commands work without a ports checkout or database; `--json` reports source/account/status or removal outcome without a token.


## Prepare verification images

Prepare the native host's conventional verification image before the first build:

```sh
dockhand setup
dockhand setup --check
dockhand setup --rebuild
dockhand setup --xcode ~/Downloads/xcode_archives
```

Setup pulls the matching vanilla Cirrus Labs macOS image, installs the pinned Tart guest agent, Apple's Command Line Tools when needed, and MacPorts, validates the result, and adopts it only after the checks pass. An existing image is validated in a disposable clone. `--rebuild` prepares a replacement while the current image remains available. A retained golden image can restore a missing image. The default profile supports arm64 macOS hosts from Monterey through Tahoe, `/opt/local`, and the Command Line Tools. `--xcode` selects the newest compatible release archive from a directory, or accepts one explicit `.xip`, and provisions a separate full-Xcode image such as `dockhand-xcode-tahoe`. Setup uses neither the ports checkout nor SQLite, but it uses the local MacPorts installation to determine the native platform.

Long Xcode uploads/expansion and Command Line Tools installation report elapsed progress. SSH connection/handshake and guest-agent readiness have bounded waits; failures retain the last useful diagnostic. A failed disposable Xcode installation leaves its expansion workspace for the provisioner to remove with the VM, rather than spending minutes recursively deleting it inside the guest.

During replacement, the stopped old image is temporarily named `<image>-previous`. A failed clone restores it automatically, including after cancellation. If the process is interrupted before restoration, setup reports the preserved name instead of silently restoring a different golden image. When the destination is missing, use `tart rename <image>-previous <image>`, then run `dockhand setup --check` with the matching OS/profile options. If both names exist, preserve the previous image until the replacement has passed an independent check; setup refuses another rebuild while that recovery image remains.

The default local image name follows the native release, such as `dockhand-base-tahoe`; an Xcode profile uses `dockhand-xcode-tahoe` and its own golden image. Verification and bump commands evaluate `use_xcode` and select the matching default image when `--image` is omitted. An explicit `--image` still takes precedence. `--source` and `--macports-version` override setup inputs. Per-image read/write locks under the Tart home allow concurrent verification clones while preventing setup from replacing their source image. A separate per-image setup lock prevents competing provisioners, including processes that selected different SQLite databases.

All cooperating drivers using the same Tart home must use the same DB, capacity, and artifact directory. A separate DB does not coordinate the shared execution pool. Base images are hashed by contents. Digests persist in SQLite across invocations and repositories; unchanged file metadata permits reuse. A new or changed image still needs a full hash. An optional `--capacity` establishes the shared pool limit. Subsequent `wait` and `start` invocations use the accepted job settings and recorded pool limit.

## Verify, follow, and cancel work

Verify current edits or committed branch contents, then reattach by port, branch, or the job ID that `-v` prints:

```sh
dockhand verify jq --working-tree --image dockhand-base-tahoe
dockhand verify jq --branch update-jq --image dockhand-base-tahoe --detach
dockhand wait --job <job_id> --trace
# Or resume every pending job already associated with a contribution:
dockhand wait --branch update-jq
# From that branch, the selector may be omitted:
dockhand wait
# Or submit and stay attached in one invocation:
dockhand verify jq --branch update-jq --image dockhand-base-tahoe
# A tracked contribution supplies the target when it is omitted:
dockhand verify --branch update-jq --image dockhand-base-tahoe

dockhand cancel --job <job_id> --wait
dockhand cancel --branch update-jq --wait
dockhand start
```

`verify <target>` continues the unique open contribution in this repository using its committed branch and recorded build settings. `--working-tree` explicitly captures tracked checkout contents, including staged additions and deletions, without changing the index or branch. Stage new files to include them. `--branch` selects committed manual work. Port and subport names are single target arguments; variants use repeated `--variant` choices.

`status` prints one row per port, its current contribution with earlier ones folded underneath: port, change, phase, state, what comes next, and the PR; `-v` prints the full record with identifiers, and `--json` carries both. On a terminal the table is live and processes the repository's work while it is open: the header says `(processing)`, the driver loop runs beside the table with its reports in the message strip, and the snapshot is reread every two seconds so rows move as work advances. The arrow keys select a row and Enter expands its branch, identifiers, PR, log, and history. `b`, `v`, `p`, `r`, `c`, and `a` run `bump` (again, continuing the contribution), `verify`, `publish`, `refresh`, `cancel`, and `abandon` on the selected contribution, exactly as the commands would; bump, verify, and publish detach as soon as the work is accepted, since the table's own processing carries it on, and bump, verify, publish, cancel, and abandon ask first. `o` opens the PR and `l` the failed build's log. Retired contributions, merged, closed, or abandoned, and finished standalone verifications are hidden until `h` shows them, and the header counts them. `--print` prints the snapshot once and processes nothing; `--json` and output that is not a terminal imply it, and `--all` includes the retired rows in any of these. Quitting the table stops its processing; accepted work stays recorded for the next `status`, `wait`, or `start`. Use `status <target>`, `wait <target>`, or `cancel <target>` for that contribution, and `--job <id>` for one particular job. `--change <id>` or `--branch <branch>` disambiguates multiple contributions. Wait/cancel freeze the pending jobs at selection; later submissions do not join. Failed preparation remains visible but cannot be verified or published until it produces a branch. A dirty checkout of the contribution branch requires explicit working-tree capture or an amendment.

Omit the port on a tracked branch to infer its single target and variants. Named continuation inherits recorded variants; explicit variant flags override them. Manual untracked branches and detached working-tree snapshots require an explicit target.

Matching passing verification is reused when the complete source tree, target, variants, image, verifier implementation, and build settings agree. You can verify edits, commit the same contents, and verify that branch without another build. Status cites the original attempt. Use `verify --fresh` to require a new execution; reattaching with `wait` preserves the existing decision. Older results without a recorded verifier identity require a fresh build before they can be reused.

Tart stages a platform-specific PortIndex generated from the frozen source instead of rebuilding the complete index inside every VM. Every consumer shares one cache of completed generations, keyed by source tree and indexing environment, in the system user cache under `dockhand/indexes`. A contribution's candidate derives from the generation of its recorded base; a new upstream master derives from the previous one; discovery, dependent selection, and staging reuse whatever generation already matches. Only the first tree in a new environment needs a full pass, and changes under `_resources` still require one. The local `portindex` is selected through `--prefix` / `MACPORTS_PREFIX`, or from `PATH`; its content identity is frozen with the accepted provider settings, and the MacPorts Base it loads is part of the cache identity.

Verification stays attached through completion; `--trace` also streams logs. With `--detach` it remains attached only while capacity is unavailable and returns at admission or a conclusive outcome. Ctrl-C detaches without canceling accepted work; `start` runs until interrupted and must be invoked separately for each repository. If nobody is running cycles for an admitted job, its VM can continue and occupy capacity until a later cycle collects its outcome. `wait` resumes a fixed job selection; it never submits another verification. With `--json`, every command writes one envelope to stdout, `{"command", "exit_code", "error", "result"}`, where `exit_code` repeats the process exit code, `error` is empty on success, and `result` is the command's typed result or null when the command was refused; progress reports become one JSON object per line on stderr with `level`, `scope`, and `message`. Without `--json`, results go to stdout and progress to stderr at the info level; `-v` adds identifiers and the work behind the scenes, `-vv` or `--debug` adds every sub-operation, and `--trace` implies debug plus the guest log stream. At the info level an action's result is one short block per job: the port, the version move or the kind of change, its state, the branch, one verdict line per platform with the failing phase and log location, and the pull request; `-v` prints the full record with identifiers instead. Exit codes are 0 for the requested milestone, 2 for failed work, 3 for needs-attention, 130 for interruption/canceled work, and 1 for other errors. Confirmed cancellation is successful for `cancel --wait`.

## End a contribution or refresh its PR

```sh
dockhand refresh jq
dockhand abandon terraform-1.16
# Inspect an older contribution after starting a newer one:
dockhand refresh --change <change_id>
```

`refresh` reads the associated PR from GitHub and records its open, closed, or merged state. A merged PR ends the contribution, so `refresh` then deletes its local branch and the head branch on your fork, each only while it still holds the published commit; a branch that is checked out or has moved is kept and named in the output. A closed PR keeps its branch, since the work may resume. `gc` later deletes local branches of merged contributions that were kept at the time. While it is open, `refresh` also records whether GitHub considers it mergeable, what reviewers decided, and how its checks stand, naming failing checks; `status` shows that line until the next refresh. Dockhand only reports it. It retires a closed or merged contribution only when no job is pending and the published revision still matches both the PR head and the local branch. Newer revisions, moved branches, and dirty checkouts stay open with an explanation. A deleted local branch or deleted fork does not prevent recognizing a matching completed PR. Plain `status` continues to read recorded state without contacting GitHub, but a processing cycle, which the live table and `start` run, looks at each open contribution's PR about every five minutes on a best-effort basis and does what `refresh` does, including retiring a merged contribution and cleaning its branches; when GitHub cannot be reached the last observation stands and nothing changes.

`abandon` explicitly ends local pursuit, including failed preparation that never made a branch. Wait for or cancel pending jobs first. It preserves branches, evidence, and any remote PR; it does not close the PR. A subsequent `bump <target>` starts a new contribution with freshly fetched source and a newly discovered release. Canceling a job alone preserves the contribution for retry.

Both commands accept a unique open port/subport target, `--branch`, or the current branch when the selector is omitted. Use `--change` for an exact contribution, including historical work. Refreshing a reopened PR never reopens a retired local contribution or redirects newer work.

`--keep-failed` keeps a failed local verification VM for investigation. The default releases it after collecting the result and logs. This is a per-job choice: `wait` preserves it, while a new verification uses its own flag. Logs remain available after VM release; see [routine cleanup](operations.md#routine-cleanup).

## Prepare version updates

Preview or prepare a version update from freshly fetched `master` in `macports/macports-ports`. Local branches and uncommitted edits are excluded; a failed fetch stops the request without falling back to stale source:

```sh
dockhand bump jq --diff
dockhand bump jq --no-verify
dockhand bump jq --no-publish
dockhand bump jq --image dockhand-base-tahoe --no-publish
dockhand bump jq 1.8.1 --diff
```

Omitting the version selects the newest eligible GitHub or GitLab version: stable releases for a port on a stable version, and prereleases as well for a port already on one, such as a `-devel` subport, using supported evaluated livecheck metadata and native MacPorts ordering. Discovery uses repository tags by default; `github.tarball_from releases` selects published GitHub releases instead. Already-current ports complete without creating a branch or starting verification. Unknown or incomplete discovery requires attention. Explicit versions also support the evaluated upstream tag prefix. The editor handles supported literal `version`, `github.setup`, `gitlab.setup`, and GitHub-backed `go.setup` sources, including the Go PortGroup’s toolchain pre-check, the version arguments of `perl5.setup`, `R.setup`, and `ruby.setup`, and one direct archive with literal checksums; see the [CLI design](cli-design.md) for limits. A version is edited in the spelling the source uses, which is `livecheck.version` when the Portfile evaluates one: a perl module version such as `0.58` is written into `perl5.setup` and the port version MacPorts derives from it, `0.580.0`, is what the release records and the commit names. CPAN's `regexm` livecheck is read like the line-oriented `regex` one. A `p5-` or `rb-` stub bumps the way a `py-` stub does, through its newest versioned subport as one shared release. A port fetched with git (`fetch.type git`) is bumped through its version alone: nothing is downloaded, the evaluated `git.branch` must land on the resolved tag, a literal commit pin is moved to the resolved commit, and the build's clone is the fetch. A git-fetched port that also declares checksums, or a generated Go or Cargo dependency block, is refused. Verification uses available dependency binaries by default; `--from-source` opts into building the dependency stack from source.

`dockhand bump py-foo` bumps a python stub the way its commits are written: the newest `py3x-foo` subport carries the edit, every subport moves as one shared release, the branch and commit keep the `py-foo` name, and only the newest subport is built locally, the pull request workflow building the rest; `--all-subports` builds them all locally, on `bump` and on `verify`. An obsolete main port that is `replaced_by` the subport being bumped and carries that subport's version as its own literal, the shape of the terraform Portfile, moves with the bump and appears as a metadata-only member of the release scope; nothing else in the Portfile is touched without `--shared-release`.

## Prepare and publish together

A plain bump prepares, verifies, and publishes as one durable job, staying in the foreground until the PR is confirmed:

```sh
dockhand bump jq
dockhand bump jq --image dockhand-base-tahoe
dockhand bump-revision jq --reason "rebuild against oniguruma 6.9.10" --trace
dockhand bump jq --detach       # submit, return at admission; wait or start finishes it
```

The destination is captured before acceptance. The driver verifies the prepared revision, then pushes it and confirms the PR. `--image` may be omitted after `setup`; Dockhand selects the matching default image. Automatic provider selection prefers a suitable Tart image and falls back to GitHub when unavailable. An explicit Tart request can reuse applicable evidence; otherwise missing build configuration preserves the prepared branch for a later verification run. With `--detach`, the command returns at build admission or evidence reuse; `wait <port>` or `start` continues the same job. Publication requires verification, so `--no-verify` also stops before the PR. Already-current automatic bumps complete without a PR. Failed verification preserves the local branch for correction and a later explicit `verify`/`publish`.

## Publish an existing branch

Publish a tracked update with `publish <target>`. You can also publish a contribution, including a branch created with ordinary Git commands, after verifying and committing its contents:

```sh
dockhand publish --branch update-jq --dry-run
dockhand publish --branch update-jq
```

Without `--branch`, publication selects the current local branch's committed contents. The first path requires one contribution commit, changes confined to one verified port directory, and passing evidence for its complete tree and target. It uses that result's recorded image, verifier, platform, variants, and build settings; no image flag or new build is needed. Missing or failed evidence requires an explicit `verify` first. A user-created branch is adopted only when publication is accepted; `--dry-run` accepts no job and creates no contribution.

Dockhand works out the remotes from their URLs and your login: the remote whose URL names `macports/macports-ports` is the upstream whatever it is called, and the remote pushing to a fork your GitHub login owns is where contributions go. `--remote` and `--upstream` override that, and Dockhand asks for `--remote` only when the choice is ambiguous: two owned forks, or several non-upstream remotes while logged out. The PR target comes from the upstream remote, then the fork parent, then the push repository, and `--base` overrides the base branch.

Publication stays attached through remote confirmation; `--detach` returns after driver pickup or an earlier terminal outcome. Resume accepted work with `wait`, or run `start`; Ctrl-C detaches. A lost PR response is reconciled by observation without repeating the write. If the outcome cannot be established, the job stays pending and reserves that remote branch. Cancellation cannot undo an already issued PR request. Missing-verification scheduling for standalone `publish`, rebase/amend commands, and post-publication monitoring remain future work.

Image inspection and fingerprinting, capacity reservation, VM startup, guest checks, source materialization, index preparation, and source transfer print stage messages on stderr. Full PortIndex generation is explicitly identified and timed; a cache hit does not claim a new generation. These messages work without `--trace` and leave JSON stdout intact. `--trace` additionally streams the build log. Stage messages describe work performed by the attached process; they are not stored progress events from other drivers.

## Upgrade an older state database

If `status` reports that the database schema needs migration, run:

```sh
dockhand db migrate
dockhand status
```

Use the same `--db PATH` on both commands when selecting a nondefault database. Migration updates the schema for every repository in that database without running jobs, accessing a ports checkout, or starting verification. An already-current schema succeeds. Missing, empty, unrelated, and newer databases are refused. To keep an old-schema snapshot first, run `dockhand db backup <new-backup-file>` with the same `--db PATH`; backup and integrity checks support older schemas without upgrading them.

## Refresh existing distfile checksums

`dockhand refresh-checksums jq --diff` previews checksum changes for the current MacPorts master without changing the port's version or revision. Omit `--diff` to prepare, verify, and publish through the normal verified publication path; `--no-publish` stops after verification. `--no-verify` stops at the prepared branch. If the checksums already match, the job completes without creating a branch or PR.

The command uses the same direct archive association, HTTP transfer, checksum replacement, and evaluation checks as version updates. Named and multiple archives are supported. A checksum group still written with `md5` or `sha1`, or without `sha256`, is rewritten as `rmd160`, `sha256`, and `size` in the Portfile's own column alignment the first time dockhand refreshes it, by this command or by a version bump; a group already made of current algorithms keeps its layout and order. Refreshing a legacy block whose archive is unchanged therefore still produces a commit: the modernized declarations. `--keep-old-checksums`, on `bump` and `refresh-checksums`, keeps a legacy block's algorithms and layout instead and refreshes every value it names, `md5` and `sha1` included; a bump that continues an open contribution keeps the choice its preparation was made with. Customized fetch hooks, authenticated downloads, and generated Go/Cargo dependency blocks require manual preparation; this command does not regenerate those blocks or turn a changed upstream archive into a trusted release automatically.

## Verify direct dependents

```sh
dockhand bump jq --dependents
dockhand verify jq --branch my-update --dependents --trace
```

`--dependents` also works with `bump-revision` and `refresh-checksums`. It requires local Tart verification and cannot be combined with `--provider github`, `--no-verify`, or `--diff`. Discovery selects the roots plus their direct build, library, and runtime dependents from the frozen source index. Reverse dependencies are not expanded transitively. The reverse index uses default-variant metadata, so it is not exhaustive coverage of every possible variant combination. Root variants are retained; downstream ports use their default variants.

Each target has an isolated guest. Before a downstream build, Dockhand builds and installs the requested roots from the same frozen tree. Ordinary dependency binaries remain available unless `--from-source` was requested. Conflicts between downstream targets therefore do not require them to coexist in one guest. Root/dependent conflicts remain real build failures and are reported.

The default image is retained for the cohort. Override individual dependent ports with repeatable `--target-image port=image`, for example:

```sh
dockhand verify root --dependents --image dockhand-base-tahoe \
  --target-image downstream=dockhand-xcode-tahoe
```

Use exact dependent names, including subport names; use `--image` for the root. Image identities and settings are frozen at intake and survive restart. Overrides must use the same OS/architecture and build policies as the root; this is not a platform matrix. A name outside the discovered cohort stops planning before builds start. Each target's full-Xcode requirement, including its root prerequisite, is checked. Missing tooling does not trigger an unrequested GitHub build or disappear from coverage.

`status` and `--json` retain each planned target, selection reasons, discovery problems, and attempts. Missing index entries or unread dependency fields mean incomplete coverage even if runnable targets pass. A build log can identify a failing dependency outside the cohort, but Dockhand does not call it unrelated without a baseline comparison.

Publication requires every requested target to pass and no discovery gaps. A later standalone `publish` using the cohort's root result enforces the same requirement. New PR bodies list the isolated coverage. This option does not authorize edits or revision bumps to downstream ports. Artifact sharing is not implemented; each guest builds its own root prerequisite.

## Correct an existing contribution

Ordinary Git edits remain supported. The managed commands squash the contribution to one commit and use the existing verification/publication lifecycle:

```sh
git switch dockhand/bump/example-...
# Edit the Portfile or patches, then stage the intended contents.
git add path/to/port/Portfile
dockhand amend --diff
dockhand amend

# Rebase without changing files in an occupied contribution checkout.
git switch master
dockhand rebase --branch dockhand/bump/example-...

# After explicitly renaming a local branch:
dockhand reassociate change_... --branch new-local-name
```

`amend` defaults to the current tracked checkout; `--branch` selects committed contents instead. Checked-out amendments require matching staged and working contents; Dockhand does not stage files or reset the checkout. Switch away before rebasing, including in linked worktrees. Rebase fetches MacPorts master, preserves one contribution commit, and leaves a conflict workspace for inspection if replay fails. Both commands retain the original contribution message (`--title` replaces its subject), verify the replacement, and accept the usual provider and `--dependents` options. With `--no-publish`, they stop after verification; `--detach` returns once the correction is accepted.

An existing PR retains its remote branch and body after local reassociation. Unexpected remote changes require reconciliation. `publish` still requires applicable verification; managed `amend` and `rebase` authorize both steps by default, and `--no-publish` stops them after verification.

## Discover upstream updates

```sh
dockhand outdated jq croc
dockhand outdated category/port --json
dockhand outdated --maintainer herbygillot@github
dockhand outdated --maintainer @herbygillot --category devel --json
```

This reads committed local `HEAD` and checks each selected port using the same GitHub/GitLab catalogs, livechecks, and calculated-version probing as bump. A livecheck is taken exactly as `port livecheck` would resolve it: the evaluator applies the tree's own checker definitions under `_resources/port1.0/livecheck`, so a `pypi`, `sourceforge`, or defaulted type becomes the URL and regex it stands for, and dockhand supports whatever comes down to a regex without custom hooks. A python stub's subports borrow the stub's livecheck, since MacPorts disables theirs when they share its version. It excludes working-tree edits and does not fetch MacPorts master, initialize a database, download source archives, create branches/jobs, or grant publication authority. Update the checkout first if you want newer MacPorts definitions.

Results distinguish `current`, `update-available`, and `unknown`. Unsupported ports and incomplete observations stay visible alongside successful results; any unknown result produces a nonzero exit status. An available update by itself is successful discovery. Automatic bump intake and unattended publication policy remain separate work.

Use explicit port arguments or metadata selectors. Repeat `--maintainer` or `--category` for alternatives within that field; combining the two fields selects their intersection. Matching is exact and case-insensitive. Maintainers accept `@handle`, Repology's `handle@github`, email addresses, and MacPorts' `domain:user` form. Categories match every indexed category, not just the Portfile directory. These selectors do not expand workflow or publication authority.

Metadata selection requires the host MacPorts `portindex` (`--prefix` selects its installation). Dockhand generates an index from the captured local HEAD in the same shared cache that verification uses, keyed by source tree, platform, and indexer identity, in the system user cache under `dockhand/indexes`. The first pass in a new indexing environment takes several minutes; later trees update incrementally from the newest cached generation. It does not use the checkout's possibly stale PortIndex, fetch master, or initialize SQLite.

Unindexed Portfiles, missing subports, and unread selection metadata remain explicit unknowns: their membership cannot be established. Selected subports currently report unknown because version probing supports primary ports only. Unsupported upstreams and individual catalog failures remain visible alongside successful results. Any unknown makes the command exit unsuccessfully after printing results; an empty, complete selection reports no matches successfully.


## Host MacPorts diagnostics

`setup` reports the host MacPorts Base and Tcl versions, evaluator startup checks, and available source-review evidence; `setup --json` includes `host_macports`. Dockhand checks actual interfaces on use. Missing metadata capabilities stop evaluation, while unknown fetch layouts or hooks stop automatic archive preparation with a specific explanation. An unfamiliar Base version is not rejected solely by its version. See [compatibility evidence](macports-compatibility.md) for the tested scope.
