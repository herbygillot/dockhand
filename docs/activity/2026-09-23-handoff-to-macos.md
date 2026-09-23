# 2026-09-23: handoff to a macOS session

This note is for the session that picks up this work on a Mac. The work so far ran in a Linux cloud container, which has no Tart, no macOS guests, and an egress proxy. Everything below builds and passes tests on Linux, both as root and as a normal user, with and without MacPorts Base on `PATH`. Anything that needs a Mac, Tart, or unrestricted network access has not been run. The main job of the next session is to run it.

The starting point is `main` at `b4a0553` (`feat(verify): build on named macOS releases with --os`). Work goes directly on `main`; there are no feature branches.

## Conventions to keep

- `AGENTS.md` is the source of truth. Before adding code, look at the package structure. Log each change in `docs/activity`. Write commits in the Conventional Commits format.
- Do not add `Co-Authored-By` trailers for an agent. Commits in this series end with:

  ```text
  Assisted-By: Claude Code
  Claude-Session: <session URL>
  ```

- Build and test with `make`, `make test`, `make test-race`, and `make vet`. Dependencies are vendored, and the Makefile sets `-mod=vendor`. `docs/development.md` lists every opt-in test variable.
- MacPorts integration tests find `port-tclsh` and `portindex` on `PATH`. Run them with `PATH=/opt/local/bin:$PATH`.

## What was done in the Linux sessions

| Commit | Change | Note |
| --- | --- | --- |
| `7da8769`, `c4cd766` | The suite passes on a Linux host and as a non-root user. Fixtures set their own git identity, and a pre-receive hook replaces the `chmod` that root ignores. Tests that need `tclsh` or `/bin/zsh` skip without them. The patch-failure regex accepts GNU and BSD wording. | `2026-09-22-tests-on-linux.md`, `2026-09-22-workflow-tests-without-host-identity.md` |
| `b548adb` | `CorrectionRequest.Tree` removed as dead. | `2026-09-22-correction-tree-removed.md` |
| `2cb5642` | A non-Mac host models darwin 25 arm64 with the Command Line Tools for Xcode 26.3. `Runtime.Host` and `Runtime.Modeled()` record the real host, and `setup` refuses on a modeled host. | `2026-09-22-linux-hosts.md` |
| `3a4a805` | GitHub tag discovery falls back to `git ls-remote` when the API fails for any reason other than a 404. | `2026-09-22-git-tag-fallback.md` |
| `13ce4df` | Architecture review of the whole tree. Most of its findings were then implemented by other sessions, in commits up to `79cdc3d`. | `docs/reviews/2026-09-23-architecture-review.md` |
| `335ed06` | `outdated` lists only out-of-date ports; `-a`/`--all` lists every port. | `2026-09-23-outdated-lists-updates.md` |
| `3b76ebe` | Cargo crate versions stay right-aligned in their block. Seen in macports-ports PR #34844, the xan bump. | `2026-09-23-crate-version-alignment.md` |
| `d1f3bc0`, `82ffb6c`, `a0df2f5` | Regressions found reviewing the implemented findings are fixed: the switch variables `-matchvar` and `-indexvar` count as writes, and GitHub log caches locked before the ledger are pruned. Tests that became redundant were removed. | `2026-09-24-review-follow-up-and-test-sweep.md` |
| `7ae28a2` | Contracts review. | `docs/reviews/2026-09-23-contracts-review.md` |
| `b4a0553` | `verify --os <release>` and `--os available`: build on named macOS releases. | `2026-09-23-verify-on-named-releases.md`, `docs/build-platforms.md` |

## What needs a Mac

The checks are in priority order. Each says what to run and what to look for.

### 1. The suite on macOS

```sh
make test
PATH=/opt/local/bin:$PATH make test
```

On Linux, the tests that need `tclsh`, `/bin/zsh`, or a real Base skipped or ran against a Base built on Linux. On a Mac they all run. Watch the patch-failure tests in `macports/patchcheck` and `macports/portedit`: their regex now accepts `hunks?` in either case, and has only been checked against GNU patch. Some `verify/tart` tests failed intermittently on Linux and passed on rerun. Note which ones fail on the Mac and how often. Per the review rules, a failing test is never put down to flakiness without a root cause.

### 2. Real Tart acceptance

```sh
DOCKHAND_TEST_TART_IMAGE=dockhand-base-tahoe \
go test -v ./internal/verify/tart -run '^TestRealTartBuildSurvivesSubmittingDriverExit$' -timeout 16m
```

This is the existing end-to-end proof. It confirms that the refactors since it last ran (the verification ledger, the index source, the scoped state) did not break a real build.

### 3. `verify --os` on real images

This feature is the least proven. Its tests use a scripted provider and fake Tart listings.

```sh
dockhand setup --os sonoma                      # a second release, beside the host's image
dockhand verify <port> --os sonoma --os tahoe -v
dockhand status <port> -v
dockhand verify <port> --os available --fresh
dockhand verify <port>                          # must build on the host alone again
dockhand verify <port> --os ventura             # no image: refusal names `setup --os ventura`
dockhand verify <port> --os sonoma --provider github   # refused before anything opens
```

What to look for:

- **Admission.** Both attempts are admitted. The guest's platform check at admission (`verify/tart/submission.go`, the comparison of `env.Platform` with `Config.Platform`) passes for the Sonoma image on a Tahoe host. This is the first time a build's platform has differed from the evaluation platform.
- **PortIndex.** The PortIndex staged for the Sonoma guest is Sonoma's. On a Mac, `portindex/index.go` passes `macports.PlatformVariables` for a darwin version that is not the host's, and only the Linux modeling path has been exercised that way. Confirm that the staged index, and a cold cache seeded from `PortIndex_darwin_23_arm`, describe darwin 23.
- **Output.** `status` and the summary print one verdict line per release. A failure on one release fails the job.
- **Capacity.** With the default capacity of 2 and three releases, the third queues rather than running beside the others.
- **Publication.** `publish` after a multi-release verify selects the host release's evidence. A verify that named only non-host releases leaves `publish` asking for a host verification.
- **Newer releases.** `--os golden-gate` is built rather than refused, if an image can be prepared. Otherwise, confirm the refusal is the setup one.
- **Hardware.** Apple's framework may refuse a guest newer than the host. That would surface in `setup`, and the design note assumes it does not reach `verify`.

### 4. Linux modeling against a real Mac

Linux describes a Mac with only the Command Line Tools for Xcode 26.3 (clang 1700.6.4.2). Run a dry-run bump on the Mac and on Linux for the same ports:

```sh
dockhand bump git-devel --dry-run -v
dockhand bump xan --dry-run -v
```

Compare the evaluated dependencies, `use_xcode`, and the compiler choice. They should match a Mac that has the same Command Line Tools and no Xcode. A Mac with Xcode installed differs, and that difference is expected. If Apple's current tools report a different clang build, update `macos.CurrentToolchain`.

### 5. End to end on git-devel

On Linux, `bump git-devel` selected `v2.56.0-rc2` through the git tag fallback, then stopped at the archive download from github.com, which the proxy refuses. On a Mac:

- Run the dry run, then a real bump through verification and publication.
- Confirm the normal path uses the GitHub API. The git fallback should appear only when the API fails, and `-v` reports each fallback to git, with the API error that caused it.

### 6. The crate alignment fix, on real output

Re-run the xan bump, or any `cargo2port` port. Check that the `cargo.crates` block keeps its right-aligned version column, compared with the diff in macports-ports PR #34844.

## Open items, for choosing what is next

From the `--os` work, recorded in `docs/build-platforms.md` under "Not decided here":

- **Per-release Xcode need.** It is read from the host evaluation. Evaluating per release would need `eval.Evaluator.Model` to model a darwin release on a darwin host; today it models only on a non-darwin host.
- **`--os` for `bump` and the corrections.** This needs a decision on what the prepared branch's recorded build is.
- **Dependents across releases.** Today `--os` is refused with `--dependents`.
- **Reuse.** Evidence reuse is not consulted when a plan has several builds.

From the contracts review's recommendations (`docs/reviews/2026-09-23-contracts-review.md`). Item 3 is done for `verify`.

1. Reword the driver prohibitions in the principles and architecture documents as first-implementation decisions.
2. Design the multi-directory contribution (3.1). It is also the roadmap's open contribution-workflow item.
3. Try the evaluator-run guard (3.5) beside the grammar, before deciding the Java `exec` policy.
4. Add a closure digest as an opt-in reuse key, and a verifier protocol version (3.2).
5. Make the workspace trace a proof (3.7).
6. Let accepted intent carry named alternatives (3.6).

From the review follow-up note, left as found:

- The Tart and GitHub providers decide the closed-request refusal each their own way.
- Four raw boolean readers remain: `cargo.update`, `go.offline_build`, `fetch.ignore_sslcert`, and the livecheck helper.
- An oversized job log fails every read instead of truncating. No test pins the 64 MiB cap or the shared fork-branch lock key.
- `TestPublishCLIAdoptsManualBranchOnlyAfterDryRun` takes about 32 seconds. It belongs in `cli`, whose fixture shortens the wait intervals.

The roadmap's ranked "Next" list, in `docs/roadmap.md`, is unchanged by this work.

## Where the `--os` code is

- **CLI:** `internal/cli/actions.go` (the verify command, the flag, and its refusals); `internal/cli/build.go` (`os` selects Tart and counts as a settings change).
- **App:** `internal/app/platforms.go` (`buildPlatforms`, `AvailablePlatforms`); `internal/app/verification.go`.
- **Tart:** `internal/tart/release.go` (`PreparedReleases`); `internal/verify/tart/config.go` (`Provider.PreparedReleases`, and `BuildOptions.Named` in `BuildConfig`).
- **Provider choice:** `internal/workflow/choice/choice.go` (`Options.Platforms`, `named`).
- **Workflow:** `internal/workflow/verification_bind.go` (`VerificationRequest.Platforms`, `BuildResolution.PlatformBuilds`, `resolvedPlatforms`); `internal/workflow/contribution_select.go` (`contributionBuild` skips jobs built on named releases); `internal/workflow/request.go` (`normalizePlatformBuilds`).
- **Record and plan:** `internal/record/job.go` (`JobSpec.PlatformBuilds`), which SQLite stores in `internal/state/sqlite/records.go`; `internal/verify/plan.go` (targets crossed with builds).

## Environment notes from the Linux sessions

- **Blocked hosts.** The cloud proxy blocked `ftp.fau.de`, the PortIndex mirror, and github.com archive downloads. It allowed GitHub API calls only for repositories attached to the session. Some commands behaved differently there for that reason alone.
- **Remote branch.** The remote branch `claude/dockhand-implementation-654e4f` could not be deleted from the container because the proxy returned 403. Delete it on GitHub if it is still there.
