# Using Dockhand

Detailed command behavior and operational choices for the current prerelease. For database backups, restoration, and resource retention, see [state operations](operations.md).

## Tools and checkout selection

Select a ports checkout with `--tree` / `-T` or `MACPORTS_TREE`; otherwise Dockhand uses the current directory. Select the local MacPorts installation with `--prefix` / `-P` or `MACPORTS_PREFIX`; otherwise it finds `port-tclsh` on `PATH`. Select Git with `--git` or `GIT_BIN`, and Tart with `--tart` or `TART_BIN`; otherwise Dockhand finds each executable on `PATH`. Flags override the environment. `--publish` has no shorthand.

## GitHub authentication

Authorize Dockhand for GitHub publication without installing `gh`:

```sh
DOCKHAND_GITHUB_CLIENT_ID=<registered-oauth-client-id> dockhand auth login
```

The OAuth application must have GitHub's device flow enabled. Build with `make GITHUB_OAUTH_CLIENT_ID=<id>` to embed its registered client ID; the client ID is public application metadata, not a secret. Login opens GitHub's device page, requests `public_repo`, validates the selected account, and stores the token in macOS Keychain. `--client-id` overrides the configured ID and `--no-browser` prints the URL for manual opening. Login uses neither the ports tree nor the workflow database.

For an ordinary build without an embedded OAuth client ID, use an existing authenticated GitHub CLI login, or provide `GH_TOKEN` or `GITHUB_TOKEN`. Git push authentication is configured separately through Git. The publication section below describes credential precedence and remote selection.

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

The default local image name follows the native release, such as `dockhand-base-tahoe`; an Xcode profile uses `dockhand-xcode-tahoe` and its own golden image. Verification and bump commands evaluate `use_xcode` and select the matching default image when `--image` is omitted. An explicit `--image` still takes precedence. `--source` and `--macports-version` override setup inputs. Per-image read/write locks under the Tart home allow concurrent verification clones while preventing setup from replacing their source image. A separate per-image setup lock prevents competing provisioners, including processes that selected different SQLite databases.

All cooperating drivers using the same Tart home must use the same DB, capacity, and artifact directory. A separate DB does not coordinate the shared execution pool. Base images are hashed by contents. Digests persist in SQLite across invocations and repositories; unchanged file metadata permits reuse. A new or changed image still needs a full hash. An optional `--capacity` establishes the shared pool limit. Subsequent `wait` and `start` invocations use the accepted job settings and recorded pool limit.

## Verify, follow, and cancel work

Verify current edits or committed branch contents, then reattach by the printed job ID:

```sh
dockhand verify jq --image dockhand-base-tahoe --wait
dockhand verify jq --branch update-jq --image dockhand-base-tahoe
dockhand wait <job_id> --trace
# Or resume every pending job already associated with a contribution:
dockhand wait --branch update-jq
# From that branch, the selector may be omitted:
dockhand wait
# Or submit and stay attached in one invocation:
dockhand verify jq --branch update-jq --image dockhand-base-tahoe --wait
# A tracked contribution supplies the target when it is omitted:
dockhand verify --branch update-jq --image dockhand-base-tahoe --wait

dockhand cancel <job_id> --wait
dockhand cancel --branch update-jq --wait
dockhand start
```

Omitting `--branch` captures tracked working-tree contents, including staged additions and deletions, without changing the index or branch. New files must be staged; explicit `--branch` uses committed contents. The accepted snapshot stays fixed while editing continues; each invocation selects one port directory or unique directory name, optionally `--subport` and repeated `--variant` choices. `wait` and `cancel` accept a job ID, `--branch`, or the current branch when both are omitted. Branch selection freezes that open contribution's queued and active jobs; later submissions do not join. Broader port selectors remain later work. A tracked contribution retains its edited targets; verification may select a different target without redefining that contribution.

Omit the port on an open tracked contribution to reuse its single target, subport, and variant choices. Explicit variant flags override the recorded choices; supplying a port starts from that port's defaults. Inference checks the selected tree against the recorded contribution base and asks for an explicit port if changes extend outside its directory. Untracked branches and detached checkouts require an explicit port. See the [inference report](activity/2026-09-13-verification-target-inference.md).

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

The destination is captured before acceptance. The driver verifies the prepared revision, then pushes it and confirms the PR. `--image` may be omitted after `setup`; Dockhand selects the matching default image. If no suitable image is available, matching recorded verification can still satisfy the accepted requirements; otherwise the job needs attention and preserves the prepared branch for a later verification run. Without `--wait` or `--trace`, the command returns at build admission or evidence reuse; `wait <job_id>` or `start` continues the same job. `--publish` requires verification and rejects `--no-verify`. Already-current automatic bumps complete without a PR. Failed verification preserves the local branch for correction and a later explicit `verify`/`publish`.

## Publish an existing branch

Publish a contribution, including a branch created with ordinary Git commands, after verifying and committing its contents:

```sh
dockhand publish --branch update-jq --dry-run
dockhand publish --branch update-jq --wait
```

Without `--branch`, publication selects the current local branch's committed contents. The first path requires one contribution commit, changes confined to one verified port directory, and passing evidence for its complete tree and target. It uses that result's recorded image, verifier, platform, variants, and build settings; no image flag or new build is needed. Missing or failed evidence requires an explicit `verify` first. A user-created branch is adopted only when publication is accepted; `--dry-run` accepts no job and creates no contribution.

The push remote defaults to `origin`; the PR target comes from `upstream` when configured, then the fork parent, then the push repository. `--remote`, `--upstream`, and `--base` override those choices. Git uses its credentials; the API reads `GH_TOKEN`, then `GITHUB_TOKEN`, Dockhand's Keychain credential, and finally the active `gh` login. Tokens never enter the workflow database or command output. Dockhand verifies the authenticated GitHub identity before accepting publication and checks again before each remote effect. The commit supplies the title and contribution description. The initial body includes the recorded build environment and a checklist based on actual verification steps; manual-review items remain unchecked. `--dry-run` shows the full body. Existing PR bodies are preserved.

Publication without `--wait` returns after driver pickup or an earlier terminal outcome. `--wait` follows remote confirmation. Resume accepted work using its job ID with `wait`, or run `start`; Ctrl-C detaches. A lost PR response is reconciled by observation without repeating the write. If the outcome cannot be established, the job stays pending and reserves that remote branch. Cancellation cannot undo an already issued PR request. Missing-verification scheduling for standalone `publish`, rebase/amend commands, and post-publication monitoring remain future work.
