<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/logo-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="images/logo-light.png">
    <img alt="dockhand logo" src="images/logo-light.png" width="480">
  </picture>
</p>

**Prepare, test, and submit changes to MacPorts ports.**

Dockhand helps contributors to [macports/macports-ports](https://github.com/macports/macports-ports) update ports, test their changes, and open pull requests. It finds upstream releases, updates Portfiles and checksums, builds in clean macOS environments, and prepares commits for MacPorts review.

Each change lives on an ordinary Git branch, with its own working directory. You can edit with your usual tools, review each step, or let Dockhand take a routine update through to a pull request. Pushes go to your fork; you do not need commit access to MacPorts.

> These examples use the v0.4.0 command interface under development on `main`. Commands may differ in earlier releases. Run `dockhand --version` to check your build and `dockhand <command> --help` for its options.

## Install

With [MacPorts](https://www.macports.org/install.php) installed:

```sh
sudo port install dockhand
```

You will also need:

- **Git 2.40 or later** and a local clone of `macports/macports-ports` with your fork configured as a remote.
- **MacPorts on your Mac**, even if you build remotely. Dockhand uses MacPorts to evaluate Portfiles.
- **GitHub access** to open pull requests and use GitHub Actions. Your Git configuration must also allow pushes to your fork without an interactive password prompt, through SSH or an HTTPS credential helper.

For local VM builds, install [Tart](https://tart.run) on an Apple silicon Mac:

```sh
sudo port install tart
```

An Intel Mac can prepare changes and check them through GitHub Actions instead. Some Go and Rust ports also need the MacPorts `go2port` or `cargo2port` helper to regenerate their dependency lists.

<details>
<summary>Build Dockhand from source</summary>

Install Go 1.27.1 or later, then:

```sh
git clone https://github.com/herbygillot/dockhand.git
cd dockhand
make build
mkdir -p "$HOME/.local/bin"
install -m 755 dockhand "$HOME/.local/bin/dockhand"
```

Make sure `~/.local/bin` is on your `PATH`. Dependencies are vendored; `make build` does not need to download Go modules.

</details>

## Set up your ports checkout

If you already have a ports checkout with your fork as a remote, run this there:

```sh
dockhand setup
```

For a new checkout, [fork macports/macports-ports](https://github.com/macports/macports-ports/fork), then clone your fork. Replace `YOUR-USERNAME` with your GitHub username:

```sh
git clone https://github.com/YOUR-USERNAME/macports-ports.git
cd macports-ports
git remote add upstream https://github.com/macports/macports-ports.git
dockhand setup
```

`setup` registers the checkout and checks your configuration. In a terminal, it offers to sign in to GitHub, save your maintainer identity, and prepare a Tart image. Creating an image downloads macOS and can use up to 60 GB of disk space; you can decline and set it up later.

You can also run those steps separately:

```sh
dockhand setup github
dockhand setup tart
```

Dockhand saves its GitHub login in the macOS Keychain. Existing `GH_TOKEN`, `GITHUB_TOKEN`, or GitHub CLI credentials can also be used; environment tokens take precedence. This API login is separate from Git's authentication for pushing commits.

Run `dockhand setup` again whenever you want to check what is ready or missing. Existing contributors can keep their remote names: Dockhand identifies upstream and your fork without requiring them to be called `upstream` and `origin`.

## Make your first update

Choose a port with a newer upstream release available. The examples below use the Rust port `ripgrep`; substitute the port you want to update. From your ports checkout:

```sh
dockhand update ripgrep --new=ripgrep-update
cd "$(dockhand path ripgrep-update)"
dockhand diff
```

`update` finds the latest release, edits the version and checksums, and regenerates supported dependency lists. It also compares the old and new source archives for changes worth reviewing, such as licensing or build requirements. If the port is already current, it creates no branch. To select a particular version, use `dockhand update ripgrep <version> --new=ripgrep-update` instead.

The branch has its own Git worktree: a separate directory sharing your checkout's Git history. Dockhand normally puts these in `~/Source/macports-branches`. Its sparse worktrees contain the ports being changed and their shared resources, so you can work on several changes without switching your main checkout between branches.

Review the diff, make any further edits, and build the result:

```sh
dockhand edit ripgrep
dockhand check
```

By default, `check` builds tracked files as they are on disk, including uncommitted edits. Stage new files or add them with `--include <file>` so the check sees them too. You do not have to commit before checking.

If you use GitHub Actions instead of Tart, run `dockhand check --on github`; enable Actions in your fork first.

When the change is ready:

```sh
dockhand tidy
dockhand submit --plan
dockhand submit
```

`tidy` prepares the commits, normally one per port directory, without changing the final contents of your files. `submit --plan` previews the destination, publishing account, commits, build results, and pull request description. `submit` then pushes to your fork and opens or updates the pull request against MacPorts.

Use `--plan` with `update`, `check`, `tidy`, or `submit` to preview that step without applying it. Submission normally requires passing checks for the committed files. A build alone does not establish that the programs work correctly or that important variants still work; include the testing you actually performed when preparing your pull request.

### If a check fails

Your branch and edits remain available. Inspect the result and log, fix the problem, and run another check:

```sh
dockhand status
dockhand logs ripgrep
dockhand edit ripgrep
dockhand check
```

`dockhand check --baseline` can help establish whether an install or test failure also occurs on the upstream version. After a reviewer requests changes, use the same edit, check, tidy, and submit cycle to update the existing pull request.

From outside the worktree, `dockhand status -p ripgrep` and `dockhand logs -p ripgrep` find the port's branch. Commands that work on a branch also accept `-b <branch>` or `--pr <number>`.

## Choose where to build

Dockhand runs lint, fetch, checksum, installation, and declared tests. Test failures are advisory by default, matching MacPorts CI. Use `--tests required` to make failed or timed-out declared tests fail the check; a port that declares no tests can still pass.

| Build environment | How it works |
| --- | --- |
| **Tart** | Builds in disposable macOS VMs on your Apple silicon Mac. The usual `host` profile selects a VM matching your Mac's macOS release. |
| **GitHub Actions** | Runs MacPorts' own workflow in your fork. Enable Actions in the fork and select `--on github`. |
| **Custom command** | Runs a script you configure for a build machine or VM you manage. |

Prepare the images you want to check against. For example, to check on Sequoia and Tahoe:

```sh
dockhand setup tart sequoia
dockhand setup tart tahoe
dockhand check --on sequoia --on tahoe
```

Each selected environment must pass. `dockhand check --build macports-ci` selects Tart releases based on MacPorts' CI workflow; you need images for those releases. Ports requiring full Xcode need an additional image, prepared with `dockhand setup tart --xcode <archive.xip>`. Without the required tools, a port is reported as not built.

For a GitHub build:

```sh
dockhand check --on github
```

This pushes a temporary branch to your fork to trigger Actions, even though you have not submitted a pull request. Dockhand saves the logs and removes the temporary branch after it has collected the results. The pull request later includes the recorded build results in its *Tested on* section.

Eligible passing results can be reused when build inputs and the environment are unchanged. Use `check --fresh` to rebuild everything.

## Run a routine update in one command

Once you are comfortable with the workflow, `ship` can update, commit, check, and submit a port for you:

```sh
dockhand ship ripgrep --plan
dockhand ship ripgrep
```

In a terminal, `ship` shows its plan and asks once before proceeding. Use `--to check` to stop after committing and checking, without opening a pull request:

```sh
dockhand ship ripgrep --to check
```

**`bump` can open a pull request without prompting:**

```sh
dockhand bump ripgrep
```

It is the unattended shortcut for a single port, distinct from MacPorts' `port bump`. Add a version argument to target a specific release. `ship --yes` and `ship` outside a terminal also proceed without confirmation. Unattended updates pause for review when they encounter major-version changes, competing pull requests, or other findings that require a decision. They use `build.unattended`, normally `minmax` for Tart: each port's oldest and newest supported macOS releases with available images. Use `--build` or `--on` to choose explicitly.

Run `ship` or `bump` again to resume an existing update. `status` shows what needs attention and suggests the next command.

## Keep up with the ports you maintain

`--mine` selects ports whose maintainer entry matches your configured identity. It refers to ports you maintain, rather than ports installed on your Mac. `setup` can suggest the identity from existing ports; you can also set it in `~/.dockhand/config.toml`:

```toml
maintainer = "{example.org:you @you} openmaintainer"
```

Replace that example with your own maintainer entry, then:

```sh
dockhand outdated --mine
dockhand ship --mine --to check
dockhand ship --passing
```

The first command lists newer releases. The second prepares and checks an update branch for each eligible port. The third submits passing branches that still need their commits published in a pull request.

For background work, run `dockhand serve`, or install it as a macOS login agent:

```sh
dockhand serve --install
```

`serve` processes queued checks, follows pull request reviews and CI, checks your ports for new releases daily, and posts macOS notifications. By default, discovering a release only adds it to status. Automatic preparation and submission are opt-in through `serve.ship_to`; automatic submissions have a daily limit and cover only branches `serve` prepared. Use `ship --passing` for branches you started yourself.

`dockhand status --watch` keeps the overview current. You can also use Dockhand without `serve`: checks run in the foreground.

## Other port work

| Task | Command |
| --- | --- |
| Start a manual fix | `dockhand edit ripgrep --new=ripgrep-fix` |
| Bring an existing branch into Dockhand | `dockhand adopt` from that branch's worktree |
| Draft a port from a GitHub project | `dockhand create <github-url> --new --category devel` |
| Find affected dependents | `dockhand impact` |
| Preview library rebuilds for an update | `dockhand update <port> --revbump-dependents --plan` |
| Compare upstream source archives | `dockhand diff --source` |
| Refresh the branch against upstream master | `dockhand rebase` |
| Undo the latest tidy or rebase | `dockhand undo` |
| Remove merged branches and their worktrees | `dockhand clean` |

New Portfiles include `# dockhand: unconfirmed` markers for inferred values. Review and correct those fields, then remove the markers before submitting. For Go and Rust version updates, prefer `update` over editing the version and running `checksums`: `checksums` alone does not regenerate dependency lists.

## Known issues in v0.4.0

Each is rough, harms nothing a look wouldn't catch, and has a workaround. Most are planned for v0.5.0, and the last nine are fixed on `main` already.

- `check` builds on this Mac's release unless told otherwise, `build.default`'s `host`, and MacPorts CI also builds on macOS 14 and 15. `submit` and the pull request say which releases no check covered; `--build macports-ci` checks on all three, and `build.default = "macports-ci"` makes it the default.
- A Go port whose module moved hosts, as pomo moved from GitHub to Codeberg, is planned without naming the move. Compare the new release's `go.mod` module path with the Portfile's before `tidy`.
- Subports that share a Portfile but differ beyond what dockhand reads, such as py-lmdb's, can get a planned edit that needs a person's look. Read `dockhand diff` before `tidy`.
- A revision that changes only a patch's contents, keeping its name, reads as fetching the base's source, so the upstream assessment compares nothing for it, that patch included. A check still applies it, and fails where it doesn't apply, so check after editing a patch.
- A Rust or Go port whose version you edit by hand keeps its old `cargo.crates` or `go.vendors`: `checksums` refreshes only its own archives' checksums. `dockhand update <port> <version>` regenerates them; undo the hand edit first.
- A batch of submits, with serve watching their pull requests, can spend GitHub's hourly limit of requests; dockhand says when it lifts.
- On a new macOS release, before MacPorts' builders have made archives of a Rust or Go toolchain, the first Tart check of a Rust or Go port there builds rust and cargo, or go, from source: hours on a VM's two cores. The check says so before it builds, and a later check installs them from the archives dockhand kept, until cleanup forgets them. Check such a port first on the previous release, `--on tahoe`, or let the first check run.
- `update` doesn't say when the new version no longer fetches an archive the old one did, such as a vendored crate it dropped; the Portfile keeps the old checksum lines. Read `dockhand diff` before `tidy`.
- A fetch during `update` that a server refuses, as a 404 does, holds the branch until a file in it is edited, even once the archive is served again; a rate limit, an outage, or mirrors not yet carrying a release are fetched again at the next look. Editing a file in the port's directory releases it.
- `serve` has printed a check's "passed" line twice once (check-130). It hasn't recurred, and it changes nothing but the output.
- `create` for a Go module older than go 1.17, whose go.sum names modules its go.mod doesn't, writes a draft with zero checksums and exits 0. Its `checksums_problem` names each row to add to `go.vendors`, with its lock; add them, with the checksums `port -v checksum` reports, before checking.
- An update whose license file keeps its licenses but gains labels, copyright lines, or terms of its own, as libxkbcommon 1.13.2's did, holds, saying the file "now reads as no license that's known". Read the license file's change with `dockhand diff --source`, then `dockhand submit --branch <name>` after a look. Fixed on `main` for v0.5.0: the hold names the licenses found and quotes the added terms, and labels and copyright lines no longer count.
- While a baseline of a passed check runs, `status` says the branch is checking, and `serve` and `ship --passing` wait to submit it. Let the baseline finish, or submit with `dockhand submit --branch <name>`. Fixed on `main` for v0.5.0.
- An update whose configure.ac names a library through a shell variable, as GraphicsMagick 1.3.49's `AC_CHECK_LIB([$zstd], ...)` does, says that library was removed. Read configure.ac's change with `dockhand diff --source` before submitting after a look. Fixed on `main` for v0.5.0.
- A failed check offers `check --baseline` for tests the branch adds and master doesn't run, where a baseline has nothing to compare. Skip the hint where the branch adds the port's tests. Fixed on `main` for v0.5.0.
- `update` refuses a port whose PortGroup reads the host while it loads, as qmake5's asks whether Xcode and an SDK are there, with "version probe is inconclusive: modeled context depends on host state", though its version line and archives read nothing of the host: LibreCAD, cpi, the PyQt5 and Qt 5 ports, and about 100 in all. Edit the version line by hand, then run `dockhand checksums <port>`. Fixed on `main` for v0.5.0.
- `dockhand update <port>` for a port that pins its source to a commit and tracks a branch, `livecheck.type git`, as wezterm does, stops with a Go panic before it changes anything, and `update <port> <version>` refuses. `outdated` says such a port's branch has moved past its pin; edit the commit and the version by hand, as it says, then `dockhand checksums <port>`. Fixed on `main` for v0.5.0: `update` moves the pin to the branch's head, or a commit named, with the version the port's scheme gives it.
- `tidy` commits a Portfile whose `patchfiles` names a new patch that Git doesn't track yet without the patch, and the check then fails fetching it. `git add` a new patch before `tidy`. `check` warns of an untracked file in a port it builds, but `ship` doesn't. Fixed on `main` for v0.5.0: `tidy` commits the patches `patchfiles` names with their Portfile and lists any other untracked file it leaves out, and `check`, `ship`, and `serve` refuse before a VM starts while a named patch is untracked.
- `logs` of a running check says it recorded no log until the target ends. `status` and `wait` show the port it's building and MacPorts' phase meanwhile. Fixed on `main` for v0.5.0: `logs` says the target is building, and the step, and that `dockhand wait` follows it.
- A diagnostic check of some of a branch's ports on a release `build.default` doesn't name, such as `check --only ufraw --on ventura`, makes `submit` ask for every port of the branch there. Check the other ports on that release too, or make a diagnostic check on a branch of its own. Fixed on `main` for v0.5.0: outside `build.default`'s releases, a port is asked for only where a check planned it.

## Scope

Dockhand currently supports updating GitHub-hosted Go and Rust ports, individually and in batches. Other port types, shared subport releases, library rebuilds, and variant-by-variant checks are outside the supported scope. They may work, but require closer review of plans and diffs. If Dockhand cannot prepare an edit automatically, it explains the limitation and any manual steps available.

`review` and `adopt --pr` are experimental: they evaluate another contributor's Portfiles with MacPorts on your Mac. Read Portfiles you do not trust before using either command; isolated evaluation is not yet available.

## Help and contributing

```sh
dockhand --help
dockhand <command> --help
dockhand config
```

For MacPorts contribution practices, see the [MacPorts contribution workflow](https://guide.macports.org/#project.github) and [commit-message guidelines](https://trac.macports.org/wiki/CommitMessages). `dockhand explain <code>` explains the rule behind a finding reported by `tidy` or `submit`.

Report Dockhand bugs and propose improvements in [this repository](https://github.com/herbygillot/dockhand). Include your `dockhand --version`, the command you ran, and the relevant error or build log. To work on Dockhand itself, build with `make build` and run its tests with `make test`.

## License

[MIT](https://github.com/herbygillot/dockhand/blob/main/LICENSE).
