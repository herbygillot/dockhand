# Using Dockhand

Detailed command behavior and operational choices for the current prerelease. For database backups, restoration, and resource retention, see [state operations](operations.md).

## Tools and checkout selection

Select a ports checkout with `--tree` / `-T` or `MACPORTS_TREE`; otherwise Dockhand uses the current directory. Select the local MacPorts installation with `--prefix` / `-P` or `MACPORTS_PREFIX`; otherwise it finds `port-tclsh` on `PATH`. Select Git with `--git` or `GIT_BIN`, and Tart with `--tart` or `TART_BIN`; otherwise Dockhand finds each executable on `PATH`. Flags override the environment. `--publish` has no shorthand.

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

Verify current edits or committed branch contents, then reattach by the printed job ID:

```sh
dockhand verify jq --working-tree --image dockhand-base-tahoe --wait
dockhand verify jq --branch update-jq --image dockhand-base-tahoe
dockhand wait --job <job_id> --trace
# Or resume every pending job already associated with a contribution:
dockhand wait --branch update-jq
# From that branch, the selector may be omitted:
dockhand wait
# Or submit and stay attached in one invocation:
dockhand verify jq --branch update-jq --image dockhand-base-tahoe --wait
# A tracked contribution supplies the target when it is omitted:
dockhand verify --branch update-jq --image dockhand-base-tahoe --wait

dockhand cancel --job <job_id> --wait
dockhand cancel --branch update-jq --wait
dockhand start
```

`verify <target>` continues the unique open contribution in this repository using its committed branch and recorded build settings. `--working-tree` explicitly captures tracked checkout contents, including staged additions and deletions, without changing the index or branch. Stage new files to include them. `--branch` selects committed manual work. Port and subport names are single target arguments; variants use repeated `--variant` choices.

Use `status <target>`, `wait <target>`, or `cancel <target>` for that contribution, and `--job <id>` for one particular job. `--change <id>` or `--branch <branch>` disambiguates multiple contributions. Wait/cancel freeze the pending jobs at selection; later submissions do not join. Failed preparation remains visible but cannot be verified or published until it produces a branch. A dirty checkout of the contribution branch requires explicit working-tree capture or an amendment.

Omit the port on a tracked branch to infer its single target and variants. Named continuation inherits recorded variants; explicit variant flags override them. Manual untracked branches and detached working-tree snapshots require an explicit target.

Matching passing verification is reused when the complete source tree, target, variants, image, verifier implementation, and build settings agree. You can verify edits, commit the same contents, and verify that branch without another build. Status cites the original attempt. Use `verify --fresh` to require a new execution; reattaching with `wait` preserves the existing decision. Older results without a recorded verifier identity require a fresh build before they can be reused.

Tart stages a platform-specific PortIndex generated from the frozen source instead of rebuilding the complete index inside every VM. Following MacPorts CI, a cold cache downloads the platform index from a MacPorts mirror, reconciles the recent base history, and then incrementally updates changed port directories for each candidate. The reconciled base index is retained; candidate indexes are temporary. Mirror failure and shared PortGroup changes fall back to a full pass. The local `portindex` is selected through `--prefix` / `MACPORTS_PREFIX`, or from `PATH`, and its content identity is frozen with the accepted provider settings.

Without `--wait`, verification remains attached while capacity is unavailable and returns at admission or a conclusive outcome. `--wait` and `--trace` follow completion. Ctrl-C detaches without canceling accepted work; `start` runs until interrupted and must be invoked separately for each repository. If nobody is running cycles for an admitted job, its VM can continue and occupy capacity until a later cycle collects its outcome. `wait` resumes a fixed job selection; it never submits another verification. JSON results go to stdout, progress and trace output to stderr. Exit codes are 0 for the requested milestone, 2 for failed work, 3 for needs-attention, 130 for interruption/canceled work, and 1 for other errors. Confirmed cancellation is successful for `cancel --wait`.

## Prepare version updates

Preview or prepare a version update from freshly fetched `master` in `macports/macports-ports`. Local branches and uncommitted edits are excluded; a failed fetch stops the request without falling back to stale source:

```sh
dockhand bump jq --diff
dockhand bump jq --no-verify
dockhand bump jq --wait
dockhand bump jq --image dockhand-base-tahoe --wait
dockhand bump jq 1.8.1 --diff
```

Omitting the version selects the newest eligible stable numeric GitHub or GitLab version using supported evaluated livecheck metadata and native MacPorts ordering. Discovery uses repository tags by default; `github.tarball_from releases` selects published GitHub releases instead. Already-current ports complete without creating a branch or starting verification. Unknown or incomplete discovery requires attention. Explicit versions also support the evaluated upstream tag prefix. The editor handles supported literal `version`, `github.setup`, `gitlab.setup`, and GitHub-backed `go.setup` sources, including the Go PortGroup’s toolchain pre-check, and one direct archive with literal checksums; see the [CLI design](cli-design.md) for limits. Verification uses available dependency binaries by default; `--from-source` opts into building the dependency stack from source.

## Prepare and publish together

Prepare, verify, and publish as one durable job:

```sh
dockhand bump jq --publish --image dockhand-base-tahoe --wait
dockhand bump-revision jq --publish --image dockhand-base-tahoe --trace
```

The destination is captured before acceptance. The driver verifies the prepared revision, then pushes it and confirms the PR. `--image` may be omitted after `setup`; Dockhand selects the matching default image. If no suitable image is available, matching recorded verification can still satisfy the accepted requirements; otherwise the job needs attention and preserves the prepared branch for a later verification run. Without `--wait` or `--trace`, the command returns at build admission or evidence reuse; `wait --job <job_id>` or `start` continues the same job. `--publish` requires verification and rejects `--no-verify`. Already-current automatic bumps complete without a PR. Failed verification preserves the local branch for correction and a later explicit `verify`/`publish`.

## Publish an existing branch

Publish a tracked update with `publish <target>`. You can also publish a contribution, including a branch created with ordinary Git commands, after verifying and committing its contents:

```sh
dockhand publish --branch update-jq --dry-run
dockhand publish --branch update-jq --wait
```

Without `--branch`, publication selects the current local branch's committed contents. The first path requires one contribution commit, changes confined to one verified port directory, and passing evidence for its complete tree and target. It uses that result's recorded image, verifier, platform, variants, and build settings; no image flag or new build is needed. Missing or failed evidence requires an explicit `verify` first. A user-created branch is adopted only when publication is accepted; `--dry-run` accepts no job and creates no contribution.

The push remote defaults to `origin`; the PR target comes from `upstream` when configured, then the fork parent, then the push repository. `--remote`, `--upstream`, and `--base` override those choices. Git uses its credentials; the API reads `GH_TOKEN`, then `GITHUB_TOKEN`, Dockhand's Keychain credential, and finally the active `gh` login. Tokens never enter the workflow database or command output. Dockhand verifies the authenticated GitHub identity before accepting publication and checks again before each remote effect. The commit supplies the title and contribution description. The initial body includes the recorded build environment and a checklist based on actual verification steps; manual-review items remain unchecked. `--dry-run` shows the full body. Existing PR bodies are preserved.

Publication without `--wait` returns after driver pickup or an earlier terminal outcome. `--wait` follows remote confirmation. Resume accepted work using its job ID with `wait`, or run `start`; Ctrl-C detaches. A lost PR response is reconciled by observation without repeating the write. If the outcome cannot be established, the job stays pending and reserves that remote branch. Cancellation cannot undo an already issued PR request. Missing-verification scheduling for standalone `publish`, rebase/amend commands, and post-publication monitoring remain future work.

Image inspection and fingerprinting, capacity reservation, VM startup, guest checks, source materialization, index preparation, and source transfer print stage messages on stderr. Full PortIndex generation is explicitly identified and timed; a cache hit does not claim a new generation. These messages work without `--trace` and leave JSON stdout intact. `--trace` additionally streams the build log. Stage messages describe work performed by the attached process; they are not stored progress events from other drivers.

## Upgrade an older state database

If `status` reports that the database schema needs migration, run:

```sh
dockhand db migrate
dockhand status
```

Use the same `--db PATH` on both commands when selecting a nondefault database. Migration updates the schema for every repository in that database without running jobs, accessing a ports checkout, or starting verification. An already-current schema succeeds. Missing, empty, unrelated, and newer databases are refused. To keep an old-schema snapshot first, run `dockhand db backup <new-backup-file>` with the same `--db PATH`; backup and integrity checks support older schemas without upgrading them.

## Refresh existing distfile checksums

`dockhand refresh-checksums jq --diff` previews checksum changes for the current MacPorts master without changing the port's version or revision. Omit `--diff` to prepare and verify a branch; `--publish --wait` uses the normal verified publication path. `--no-verify` stops at the prepared branch. If the checksums already match, the job completes without creating a branch or PR.

The command uses the same direct archive association, HTTP transfer, checksum replacement, and evaluation checks as version updates. Named and multiple archives are supported. Customized fetch hooks, authenticated downloads, and generated Go/Cargo dependency blocks require manual preparation; this command does not regenerate those blocks or turn a changed upstream archive into a trusted release automatically.

## Verify direct dependents

```sh
dockhand bump jq --dependents --wait
dockhand verify jq --branch my-update --dependents --trace
```

`--dependents` also works with `bump-revision` and `refresh-checksums`. It requires local Tart verification and cannot be combined with `--provider github`, `--no-verify`, or `--diff`. Discovery selects the roots plus their direct build, library, and runtime dependents from the frozen source index. Reverse dependencies are not expanded transitively. The reverse index uses default-variant metadata, so it is not exhaustive coverage of every possible variant combination. Root variants are retained; downstream ports use their default variants.

Each target has an isolated guest. Before a downstream build, Dockhand builds and installs the requested roots from the same frozen tree. Ordinary dependency binaries remain available unless `--from-source` was requested. Conflicts between downstream targets therefore do not require them to coexist in one guest. Root/dependent conflicts remain real build failures and are reported.

The default image is retained for the cohort. Override individual dependent ports with repeatable `--target-image port=image`, for example:

```sh
dockhand verify root --dependents --image dockhand-base-tahoe \
  --target-image downstream=dockhand-xcode-tahoe --wait
```

Use exact dependent names, including subport names; use `--image` for the root. Image identities and settings are frozen at intake and survive restart. Overrides must use the same OS/architecture and build policies as the root; this is not a platform matrix. A name outside the discovered cohort stops planning before builds start. Each target's full-Xcode requirement, including its root prerequisite, is checked. Missing tooling does not trigger an unrequested GitHub build or disappear from coverage.

`status` and `--json` retain each planned target, selection reasons, discovery problems, and attempts. Missing index entries or unread dependency fields mean incomplete coverage even if runnable targets pass. A build log can identify a failing dependency outside the cohort, but Dockhand does not call it unrelated without a baseline comparison.

`--publish` requires every requested target to pass and no discovery gaps. A later standalone `publish` using the cohort's root result enforces the same requirement. New PR bodies list the isolated coverage. This option does not authorize edits or revision bumps to downstream ports. Artifact sharing is not implemented; each guest builds its own root prerequisite.

## Correct an existing contribution

Ordinary Git edits remain supported. The managed commands squash the contribution to one commit and use the existing verification/publication lifecycle:

```sh
git switch dockhand/bump/example-...
# Edit the Portfile or patches, then stage the intended contents.
git add path/to/port/Portfile
dockhand amend --diff
dockhand amend --publish --wait

# Rebase without changing files in an occupied contribution checkout.
git switch master
dockhand rebase --branch dockhand/bump/example-... --publish --wait

# After explicitly renaming a local branch:
dockhand reassociate change_... --branch new-local-name
```

`amend` defaults to the current tracked checkout; `--branch` selects committed contents instead. Checked-out amendments require matching staged and working contents; Dockhand does not stage files or reset the checkout. Switch away before rebasing, including in linked worktrees. Rebase fetches MacPorts master, preserves one contribution commit, and leaves a conflict workspace for inspection if replay fails. Both commands retain the original contribution message (`--title` replaces its subject), verify the replacement, and accept the usual provider and `--dependents` options. Without `--publish`, they stop after verification.

An existing PR retains its remote branch and body after local reassociation. Unexpected remote changes require reconciliation. `publish` still requires applicable verification; managed `amend --publish` and `rebase --publish` authorize both steps.

## Discover upstream updates

```sh
dockhand outdated jq croc
dockhand outdated category/port --json
dockhand outdated --maintainer herbygillot@github
dockhand outdated --maintainer @herbygillot --category devel --json
```

This reads committed local `HEAD` and checks each selected port using the same GitHub/GitLab source conventions and calculated-version probing as bump. It excludes working-tree edits and does not fetch MacPorts master, initialize a database, download source archives, create branches/jobs, or grant publication authority. Update the checkout first if you want newer MacPorts definitions.

Results distinguish `current`, `update-available`, and `unknown`. Unsupported ports and incomplete observations stay visible alongside successful results; any unknown result produces a nonzero exit status. An available update by itself is successful discovery. Automatic bump intake and unattended publication policy remain separate work.

Use explicit port arguments or metadata selectors. Repeat `--maintainer` or `--category` for alternatives within that field; combining the two fields selects their intersection. Matching is exact and case-insensitive. Maintainers accept `@handle`, Repology's `handle@github`, email addresses, and MacPorts' `domain:user` form. Categories match every indexed category, not just the Portfile directory. These selectors do not expand workflow or publication authority.

Metadata selection requires the host MacPorts `portindex` (`--prefix` selects its installation). Dockhand generates an index from the captured local HEAD and caches it by source tree, platform, and indexer identity in the system user cache under `dockhand/indexes`. The first pass on a full tree may take several minutes. It does not use the checkout's possibly stale PortIndex, fetch master, or initialize SQLite.

Unindexed Portfiles, missing subports, and unread selection metadata remain explicit unknowns: their membership cannot be established. Selected subports currently report unknown because version probing supports primary ports only. Unsupported upstreams and individual catalog failures remain visible alongside successful results. Any unknown makes the command exit unsuccessfully after printing results; an empty, complete selection reports no matches successfully.


## Host MacPorts diagnostics

`setup` reports the host MacPorts Base and Tcl versions, evaluator startup checks, and available source-review evidence; `setup --json` includes `host_macports`. Dockhand checks actual interfaces on use. Missing metadata capabilities stop evaluation, while unknown fetch layouts or hooks stop automatic archive preparation with a specific explanation. An unfamiliar Base version is not rejected solely by its version. See [compatibility evidence](macports-compatibility.md) for the tested scope.
