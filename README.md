> [!NOTE]
> **This is dockhand v0.3.1, the first point release of its third design.** Commands, output, and configuration still change in 0.x releases, and v0.4.0, which `main` is moving toward, changes some: `-b` names only an existing branch, with `--new=<name>` to start one, the hidden `auth` command goes, with `setup` naming your GitHub login, `ship` takes a change from release to pull request in one command, replacing `update --submit`, `update --outdated`, `submit --check`, and `submit --passing`, with `serve.ship_to` replacing `serve.for_outdated` and `serve.submit_passing`; `queue` and `watch` fold into `status`; `status`, `path`, `archive`, and the run commands take `-b`, `-p`, and `--pr`; and `logs --all`, `outdated --all`, and `diff --archive` become `--full`, `--current`, and `--source`. From v0.3.0, upgrade: since MacPorts 2.12.7, v0.3.0's `setup` can't make a Tart image, and v0.3.1's can. Its database, `~/.dockhand/dockhand.db`, is migrated forward by each newer dockhand and never discarded.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/logo-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="images/logo-light.png">
    <img alt="dockhand logo" src="images/logo-light.png" width="480">
  </picture>
</p>

# dockhand

**From upstream release to submitted port change.**

```sh
dockhand bump jq                  # jq to its newest release, built in a clean macOS VM,
                                  # one commit, and its pull request opened from your fork
```

Dockhand is for people who keep [MacPorts](https://www.macports.org) ports up to date and contribute their changes as pull requests. It works on branches of your ports checkout. A branch can hold one port's update, a new port, a patch fix, or a library update together with the rebuilds of the ports that link it. Dockhand moves ports to new releases and fills in their checksums, and builds what the branch changes in a clean macOS virtual machine. It then shapes the commits the way MacPorts asks for them and opens the pull request from your fork. You edit in the branch's own worktree with your own tools, and nothing leaves your Mac until a command that submits, `bump`, `ship`, or `submit`, opens the pull request.

Two things a MacPorts contributor will want to know first:

- **It works entirely through your own fork** of `macports/macports-ports`. The pull request it opens is the same kind you would open by hand, and nothing is ever pushed to the MacPorts repository. You do not need commit access.
- **It builds the way MacPorts CI builds.** It lints each port, fetches and checks its distfiles, and installs it, in dependency order. Those steps decide the result. Declared tests run and their result is reported, but a failing test suite does not fail the check, which is MacPorts' own rule; `--tests required` makes tests count.

When a Portfile does something dockhand does not understand, or a build fails, or an upstream release looks wrong, it stops, keeps what it has done, and says why. It never guesses.

## Getting started

```sh
cd ~/Source/macports-ports        # your clone, with your fork as a remote
dockhand setup                    # register this checkout, then offer the GitHub login,
                                  # your maintainers line, and a macOS image to build in
```

The maintainers line is offered as the ports maintained by your GitHub login write it; with none, setup says how to write it yourself. `setup tart` and `setup github` do the image and the login by themselves. `setup` finds the remote for `macports/macports-ports` and puts branch worktrees in `~/Source/macports-branches`, unless you say otherwise. Your own checkout is left alone: each branch gets a sparse worktree of its own, holding only `_resources` and the ports it changes.

## An update in one command

```sh
dockhand bump jq                  # the newest release
dockhand bump jq 1.8.1            # or the version named
```

`bump` takes a port from its upstream release to an open pull request and asks nothing: it starts a branch from fresh master, updates the port and its checksums, builds it in a clean VM, tidies the edit into one commit, and submits that commit once the check passes. It stops wherever a person should look, and keeps what it has done: a new major version, another pull request open for the port, a failed check, or an upstream archive that changed in a way a build can't catch. Each stop says why, and what goes on from there. `ship jq` is the same, showing its plan and asking once on a terminal; `ship --to check` stops after the check.

## Step by step

```sh
dockhand update jq --new          # a branch from fresh master, jq moved to its newest release
cd "$(dockhand path jq-…)"        # the branch's worktree, where the next steps find it
dockhand check                    # build it in a clean VM of this Mac's macOS
dockhand tidy                     # one commit, "jq: update to 1.8.1"
dockhand submit                   # push to your fork and open the pull request
```

`update` changes only the branch's own files, which `dockhand diff` shows, and the steps after it, which build, commit, and push, each show what they will do before they do it; `--plan` on any of them shows the plan and changes nothing. Run again, `bump` and `ship` resume the branch where it stands. If the check fails, the branch stays as it is. `dockhand logs check-12 jq` prints the log, `dockhand logs -p jq` the same from anywhere, and you fix the Portfile in the worktree and run `check` again. Checks build the files as they are on disk, committed or not, so there is nothing to commit first; `--head` builds only the commits.

`status` shows every open branch: its ports, its edits, its latest check, and its pull request, under a list of what needs you, each with the command that moves it forward. Inside a branch's worktree, `status` shows that branch in detail.

## Where checks build

```sh
dockhand setup                    # what is set up, and what is missing
dockhand check --on sequoia --on tahoe      # both releases, and both must pass
dockhand check --build macports-ci          # MacPorts CI's releases: sonoma, sequoia, tahoe
```

- **tart**, the default unless you've set up a command provider: a fresh clone of dockhand's own image for each macOS release, deleted afterwards. It needs an Apple silicon Mac with [Tart](https://tart.run) (`sudo port install tart`). The first image is this Mac's release; `setup tart sequoia` adds another. Ports that need the full Xcode build only in an Xcode image, which `setup tart --xcode ~/Downloads/Xcode_26.xip` adds beside the release's base image, made first. Without one, those ports are reported as not built, and never as failed.
- **github**: MacPorts' own CI workflow, run in your fork's GitHub Actions. It needs the GitHub login and the workflow enabled in your fork.
- **command**: your own script, for a build box or a VM you manage. See [the command provider](docs/command-provider.md).

The pull request's *Tested on* section says what each environment was, down to the macOS build and Xcode version the guest reported, and gives the ID of each run, which `dockhand logs` looks up.

## Keeping up with your ports

```sh
dockhand outdated --mine                  # which of your ports have newer releases
dockhand ship --mine --to check           # one branch and one commit each, each checked
dockhand ship --passing                   # submit the ones that passed
```

`--mine` means the ports whose `maintainers` line names you, from `maintainer` in `~/.dockhand/config.toml`. Left running, `dockhand serve` runs the checks you queue, such as with `check -d`, looks for new releases of your ports each day and lists them, unless `serve.ship_to` says to carry them further, as `ship --to` does, and follows your pull requests' reviews and CI. It posts macOS notifications as checks finish and pull requests change, and `serve --install` makes it a launchd agent that starts at login; when dockhand is upgraded, the agent finishes its checks and starts again on the new build. It acts on GitHub only as the account it started as: logged out, it reads pull requests without a login and opens, pushes, and checks there nothing until you log in again. It opens no pull requests unless you ask it to, with `serve.ship_to = "submit"`, and then at most `serve.submit_limit` a day.

## Other kinds of change

| Command | What it does |
| --- | --- |
| `create <url>` or `create crates:NAME`, `pypi:`, `go:` | writes a new port's first Portfile from its project or its registry name, marking what it guessed |
| `revbump <port>... --subject "rebuild for …"` | bumps revisions for a rebuild, with the reason as the commit subject |
| `update <port> --revbump-dependents` | also bumps the ports that link the updated library |
| `checksums <port>` | fills in checksums after you edit a version by hand, and handles stealth updates |
| `edit <port>` | opens a port's Portfile, adding its directory to a sparse worktree |
| `adopt` | tracks a branch you made yourself |
| `adopt --pr <number>` | brings someone's pull request into a branch of its own |
| `review <pr>` | applies MacPorts' commit rules to a pull request, posting nothing unless asked |
| `diff`, `impact` | what the branch changes, and which other ports that reaches |
| `rebase` | moves the branch onto fresh master, keeping a checkpoint `undo` brings back |
| `clean` | removes merged branches' worktrees and branches, here and in your fork |

## What it supports

This release supports **updating ports whose source is on GitHub and that build with Go or Rust**, one port at a time and in batches: `update`, `check`, `tidy`, `submit`, `ship`, `bump`, and `ship --mine`. Every other Portfile shape either works or refuses with a reason; dockhand never makes an edit it can't stand behind. Shapes outside that scope, which it plans with care or refuses:

- ports on another forge, such as GitLab or Codeberg, and ports MacPorts fetches with Git;
- Python ports, and ports whose subports share a revision or a release;
- C libraries with `--revbump-dependents`, and ports checked variant by variant.

Read what `--plan` shows for these before you go on. Two commands are **experimental** until dockhand runs another person's Portfile apart from your own: `adopt --pr` and `review` evaluate a pull request's Portfiles with MacPorts on your Mac, and say so each time.

Building in Tart needs an Apple silicon Mac. An Intel Mac, like any other, prepares changes here and checks them on GitHub Actions, `--on github`, which runs MacPorts' own workflow in your fork.

## Known issues in v0.4.0

Each is rough, harms nothing a look wouldn't catch, and has a workaround. v0.4.0 fixed eight of v0.3.1's: a GitHub tag moved after its check, the account a `GH_TOKEN` submits as, who requested changes in `submit --json`, `dockhand path` for a merged branch clean kept, terraform's stub wording, an old Tart in `check` and `serve`, a deleted base image's restore, and a guest's error tried again three times where it isn't the VM's. Most of the rest are planned for v0.5.0.

- `check` builds on this Mac's release unless told otherwise, `build.default`'s `host`, and MacPorts CI also builds on macOS 14 and 15. `submit` and the pull request say which releases no check covered; `--build macports-ci` checks on all three, and `build.default = "macports-ci"` makes it the default.
- A Go port whose module moved hosts, as pomo moved from GitHub to Codeberg, is planned without naming the move. Compare the new release's `go.mod` module path with the Portfile's before `tidy`.
- Subports that share a Portfile but differ beyond what dockhand reads, such as py-lmdb's, can get a planned edit that needs a person's look. Read `dockhand diff` before `tidy`.
- A revision that changes only a patch's contents, keeping its name, reads as fetching the base's source, so the upstream assessment compares nothing for it, that patch included. A check still applies it, and fails where it doesn't apply, so check after editing a patch.
- A port fetched with Git whose `git.url` changes while its tag or branch stays reads as the same source, so the upstream assessment compares nothing for it. Read `dockhand diff` before `tidy`.
- When `update` drops a patch upstream merged, it checks only that the port's patches, version, and revision changed as meant, not its other declarations or its subports'. Read `dockhand diff` before `tidy` where it drops one.
- A Rust or Go port whose version you edit by hand keeps its old `cargo.crates` or `go.vendors`: `checksums` refreshes only its own archives' checksums. `dockhand update <port> <version>` regenerates them; undo the hand edit first.
- A batch of submits, with serve watching their pull requests, can spend GitHub's hourly limit of requests; dockhand says when it lifts.
- On a new macOS release, before MacPorts' builders have made archives of a Rust or Go toolchain, the first Tart check of a Rust or Go port there builds rust and cargo, or go, from source: hours on a VM's two cores (dust on macOS 27, 2026-10-04). The check says so before it builds, as "rust, cargo: no archive for macOS 27 arm64; building from source", and a later check installs them from the archives dockhand kept, until cleanup forgets them. Check such a port first on the previous release, `--on tahoe`, or let the first check run.
- `update` doesn't say when the new version no longer fetches an archive the old one did, such as a vendored crate it dropped; the Portfile keeps the old checksum lines. Read `dockhand diff` before `tidy`.
- A fetch during `update` that a server refuses, as a 404 does, holds the branch until a file in it is edited, even once the archive is served again; a rate limit or an outage is fetched again at the next look. Editing a file in the port's directory releases it.
- `logs` of a running check says it recorded no log until the target ends. `status` and `wait` show the port it's building and MacPorts' phase meanwhile.
- `serve` has printed a check's "passed" line twice (check-130, field testing, 2026-10-02). It hasn't recurred, and it changes nothing but the output.
- `create` for a Go module older than go 1.17, whose go.sum names modules its go.mod doesn't, writes a draft with zero checksums and exits 0. Its `checksums_problem` names each row to add to `go.vendors`, with its lock; add them, with the checksums `port -v checksum` reports, before checking.

## Requirements

- **macOS on Apple silicon** to build in Tart. Any Mac can prepare changes and check them on GitHub Actions or your own script.
- **MacPorts.** Dockhand reads Portfiles through MacPorts' own Tcl interpreter, so it sees what `port` sees.
- **Git 2.40 or newer, and a clone of your fork** of `macports/macports-ports` that Git can push to. `setup` checks the Git it runs: `git` on `PATH`, or `$GIT_BIN`. Dockhand finds the upstream remote whatever it is called, and your fork by your GitHub login.
- **A GitHub login** for `submit`: `dockhand setup github`, `GH_TOKEN` or `GITHUB_TOKEN`, or the GitHub CLI's.
- **`go2port` or `cargo2port`**, from MacPorts, only for Go and Rust ports whose Portfile lists its dependencies.

### Installing

With MacPorts:

```sh
sudo port install dockhand
```

Or build from source with Go 1.27.1 or newer. The dependencies are vendored, so the build needs no network:

```sh
git clone --branch v0.3.1 https://github.com/herbygillot/dockhand.git
cd dockhand
make build
mkdir -p "$HOME/.local/bin" && install -m 755 dockhand "$HOME/.local/bin/dockhand"
```

`dockhand --version` shows which build you have.

## More

[`docs/usage.md`](docs/usage.md) is the guide to every command, setting, and provider. [`docs/design-v3.md`](docs/design-v3.md) is the design and its reasons, [`docs/architecture.md`](docs/architecture.md) maps it onto the code, and [`docs/roadmap.md`](docs/roadmap.md) says what comes next. `dockhand <command> --help` is the reference for each command, and `dockhand config` shows every setting in effect.

Dockhand is developed at [github.com/herbygillot/dockhand](https://github.com/herbygillot/dockhand) and licensed under the [MIT License](LICENSE).
