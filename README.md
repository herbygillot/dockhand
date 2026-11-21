<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/logo-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="images/logo-light.png">
    <img alt="dockhand logo" src="images/logo-light.png" width="480">
  </picture>
</p>

**Prepare, test, and submit changes to MacPorts ports.**

Dockhand automates the routine work of contributing to [macports/macports-ports](https://github.com/macports/macports-ports). It finds upstream releases, updates Portfiles, checksums, and Go/Cargo dependency lists, builds in clean macOS environments, shapes MacPorts-compliant commits, and opens pull requests from your fork.

Every change lives on an ordinary Git branch in its own worktree, so your main ports checkout stays untouched. Pushes go only to your fork—you do not need commit access to MacPorts, and Dockhand never merges or pushes upstream.

## Install

```sh
sudo port install dockhand
```

**Requirements:** macOS with [MacPorts](https://www.macports.org/install.php), **Git 2.40+**, and a GitHub account with a fork of `macports/macports-ports` (configured for non-interactive pushes via SSH or a credential helper).

- **Local VM builds (Apple silicon, recommended):** install [Tart](https://tart.run) with `sudo port install tart`. On Intel Macs—or without local VMs—you can build with GitHub Actions (`--on github`) or your local MacPorts prefix (`--on local`).
- **Go and Rust ports:** install `go2port` or `cargo2port` via MacPorts to regenerate `go.vendors` and `cargo.crates`.

<details>
<summary>Build from source (v0.8.0) or configure shell completion</summary>

Requires Go 1.27.1 or later (dependencies are vendored):

```sh
git clone --branch v0.8.0 https://github.com/herbygillot/dockhand.git
cd dockhand
make build
mkdir -p "$HOME/.local/bin"
install -m 755 dockhand "$HOME/.local/bin/dockhand"
```

Ensure `~/.local/bin` is on your `PATH`. For shell completion setup, the help of `completion zsh`, `completion bash` or `completion fish` says how.

</details>

## Set up your checkout

In an existing `macports-ports` checkout (any remote names work), run:

```sh
dockhand setup
```

Starting fresh? [Fork `macports/macports-ports`](https://github.com/macports/macports-ports/fork), clone your fork, and add `upstream`:

```sh
git clone https://github.com/YOUR-USERNAME/macports-ports.git
cd macports-ports
git remote add upstream https://github.com/macports/macports-ports.git
dockhand setup
```

`setup` registers the checkout and walks you through signing in to GitHub (stored in the macOS Keychain; `GH_TOKEN`/`GITHUB_TOKEN` and `gh` credentials also work), saving your maintainer identity (detected automatically if you already maintain ports, or configurable later in `~/.dockhand/config.toml` for `--mine` and `create`), and optionally preparing a Tart macOS VM image (up to 60 GB of disk space). You can skip any step and run `dockhand setup github`, `dockhand setup tart`, or `dockhand setup` again later.

## Update a port

Choose how much of the workflow you want Dockhand to drive. The examples below use `ripgrep`; substitute any port.

### End to end in one command: `bump`

For a routine upstream release, `dockhand bump` takes a single port from A to Z without prompts:

```sh
dockhand bump ripgrep            # update to the latest release, check, and open a PR
dockhand bump ripgrep <version>  # target a specific release
dockhand bump ripgrep --plan     # preview without changing anything
```

`bump` creates a branch and worktree from fresh `master`, updates the Portfile (version, checksums, and supported dependency lists), compares the old and new upstream archives for license or build changes, commits, builds and tests the port, and opens a pull request from your fork.

It is designed to fail safe: if it encounters a major-version jump, a competing pull request, a build failure, or an archive diff that needs human judgment, it **stops and holds the branch** for your review rather than submitting. Inspect what stopped it with `dockhand status` and `dockhand logs ripgrep`.

> Unlike MacPorts' `port bump` (which only refreshes checksums), `dockhand bump` runs a full version update and submission. Use `dockhand checksums` to refresh checksums alone.

### Piecemeal and batch control: `ship`

`dockhand ship` runs the same pipeline as `bump`, but lets you preview the plan, confirm before starting, stop at an earlier stage, or work across multiple ports:

```sh
dockhand ship ripgrep --plan        # inspect the plan without making changes
dockhand ship ripgrep --to commit   # stop after updating and committing
dockhand ship ripgrep --to check    # stop after building and testing (no PR yet)
dockhand ship ripgrep               # resume and open the PR (asks once first)
```

Running `ship` again on the same port or branch (`dockhand ship -b <branch>`) **resumes where it left off**, skipping stages that already passed.

It also scales to your maintained ports (configured via `maintainer` in `~/.dockhand/config.toml`) or any maintainer/category selection:

```sh
dockhand outdated --mine            # list newer upstream releases for your ports
dockhand ship --mine --to check     # prepare and build all eligible updates
dockhand ship --passing             # open PRs for branches whose checks passed
```

### Step by step (manual edits and fixes)

When you want to inspect diffs, edit Portfiles by hand, or fix a broken build, run each stage directly inside the branch's worktree (by default under `~/Source/macports-branches`):

```sh
dockhand update ripgrep --new       # create a branch/worktree with the new release
cd "$(dockhand path -p ripgrep)"    # enter the worktree
dockhand diff                       # inspect Portfile and upstream changes
dockhand edit ripgrep               # open the Portfile in your editor
dockhand check                      # build and test working files
dockhand tidy                       # shape commits for MacPorts review
dockhand submit --plan              # preview the push, build evidence, and PR body
dockhand submit                     # push to your fork and open/update the PR
```

`check` builds tracked working files (including uncommitted edits), so you can iterate before committing. If a check fails or a reviewer asks for changes, inspect `dockhand status` and `dockhand logs ripgrep`, edit, re-check (`dockhand check --baseline` checks whether `master` fails the same way), and run `dockhand submit` again to update the PR.

## Where builds run

Checks run lint, fetch, checksum, install from source, and declared tests (test failures are advisory by default, matching MacPorts CI; pass `--tests required` to fail on them). Passing builds are reused automatically when inputs haven't changed.

| Environment | Flag / Setup | How it works |
| --- | --- | --- |
| **Tart** *(default on Apple silicon)* | `dockhand setup tart [release]` | Builds in disposable macOS VMs. The default `minmax` profile covers each port's oldest and newest available images; use `--on <release>` or `--build macports-ci` for specific releases. See [Tart provider](docs/tart-provider.md). |
| **GitHub Actions** | `--on github` | Runs MacPorts' CI workflow in your fork via a temporary branch (cleaned up automatically). Enable Actions in your fork first. See [GitHub provider](docs/github-provider.md). |
| **Local prefix** | `--on local` | Builds in your local MacPorts prefix (`/opt/local`, or `--on local:<prefix>`) and restores active ports afterward. See [local provider](docs/local-provider.md). |
| **Custom command** | Configured profile | Runs a script you define, such as building on another Mac over SSH. See [command provider](docs/command-provider.md). |

## Stay on top of your ports

- **`dockhand console`** — live terminal dashboard of your branches, PR reviews, CI status, and MacPorts' 72-hour maintainer timeout, with single-key shortcuts for `logs` (`l`), `check` (`c`), `tidy` (`t`), and `submit` (`s`).
- **`dockhand serve --install`** — optional macOS login agent that processes queued checks, tracks open PRs, checks your maintained ports daily for new releases, and sends macOS notifications. Automatic preparation and submission are opt-in via `serve.ship_to` in `~/.dockhand/config.toml`.

### Other common tasks

| Task | Command |
| --- | --- |
| Start a manual Portfile fix | `dockhand edit <port> --new` |
| Draft a new port (GitHub, PyPI, crates.io, Go) | `dockhand create <source> --new --category <cat>` |
| Check dependents and library rebuilds | `dockhand impact`, or `dockhand update <port> --revbump-dependents --plan` |
| Compare old and new upstream archives | `dockhand diff --source` |
| Review someone else's PR (optionally build in Tart) | `dockhand review <pr>` / `dockhand review <pr> --check` |
| Bring an existing Git branch into Dockhand | `dockhand adopt` (from that branch's worktree) |
| Refresh a branch against `master` or undo a tidy/rebase | `dockhand rebase` / `dockhand undo` |
| Open a shell in a kept Tart check VM | `dockhand check --keep`, then `dockhand shell <check-id>` |
| Clean up merged branches and worktrees | `dockhand clean` |

## What Dockhand handles best

Dockhand is built for everyday MacPorts maintenance: routine version updates from GitHub releases and GitLab tags—especially Go (`go.vendors`), Rust (`cargo.crates`), and C/C++ (CMake, Meson, autotools) ports—along with batch maintenance, library revbumps, and drafting new ports.

For ports with custom version calculations, shared multi-subport Portfiles, or upstreams outside GitHub and GitLab, Dockhand either produces a conservative plan or refuses clearly and tells you what to finish by hand. See the [v0.8.0 release notes](docs/releases/v0.8.0.md) for current changes and known limitations.

## Documentation and help

Dockhand's full workflow guide is built into the binary and available in [`docs/usage/`](docs/usage.md):

```sh
dockhand --help           # command overview
dockhand <cmd> --help     # flags and options for any command
dockhand usage            # first-run guide
dockhand usage <topic>    # deep-dive workflow guides
dockhand explain <code>   # explain any finding, check result, or branch state
```

| Guide (`dockhand usage <topic>`) | Covers |
| --- | --- |
| [First run to PR](docs/usage/setup.md) (`usage`) | Setup, authentication, providers, and first update |
| [Branches & worktrees](docs/usage/branches.md) (`branches`) | Finding, adopting, rebasing, undoing, and cleaning branches |
| [Hand edits](docs/usage/hand-edits.md) (`hand-edits`) | Editing Portfiles, refreshing checksums, and revbumping dependents |
| [Builds & tests](docs/usage/checks.md) (`checks`) | Capture modes, variants, test policies, and build targets |
| [Publishing](docs/usage/publishing.md) (`publishing`) | Shaping commits, review updates, drafts, and evidence rules |
| [Recovery](docs/usage/recovery.md) (`recovery`) | Diagnosing holds, failed builds, and stale evidence |
| [Batches](docs/usage/batches.md) (`batches`) | Selecting and shipping many ports at once |
| [Unattended work](docs/usage/unattended.md) (`unattended`) | `bump`, background checks, `serve`, and holds |
| [New ports](docs/usage/new-port.md) (`new-port`) | Creating and finishing a new Portfile |
| [PR reviews](docs/usage/review.md) (`review`) | Inspecting, adopting, and building others' PRs |
| [Configuration](docs/usage/configuration.md) (`configuration`) | Build profiles, holds, tools, paths, and retention |
| [Scripts & agents](docs/usage/agents.md) (`agents`) | `--json` output, exit codes, and non-interactive automation |

For MacPorts conventions, see the [MacPorts contribution workflow](https://guide.macports.org/#project.github) and [commit-message guidelines](https://trac.macports.org/wiki/CommitMessages).

## Contributing and license

Report bugs or suggest improvements on [GitHub Issues](https://github.com/herbygillot/dockhand/issues) (include `dockhand --version` or `-V`, the command you ran, and the relevant error or log). To work on Dockhand itself, build with `make build` and run tests with `make test`.

Licensed under the [MIT License](LICENSE).
