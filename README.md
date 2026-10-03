> [!NOTE]
> **This is dockhand v3, and it is new.** Its commands, output, and configuration may still change before it is released. Its database, `~/.dockhand/dockhand.db`, is migrated forward by each newer dockhand and never discarded. v2 is kept at the tag `v2-final`; the two never share a database.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/logo-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="images/logo-light.png">
    <img alt="dockhand logo" src="images/logo-light.png" width="480">
  </picture>
</p>

# dockhand

**From upstream release to submitted port change.**

Dockhand is for people who keep [MacPorts](https://www.macports.org) ports up to date and contribute their changes as pull requests. It works on branches of your ports checkout. A branch can hold one port's update, a new port, a patch fix, or a library update together with the rebuilds of the ports that link it. Dockhand moves ports to new releases and fills in their checksums, and builds what the branch changes in a clean macOS virtual machine. It then shapes the commits the way MacPorts asks for them and opens the pull request from your fork. You edit in the branch's own worktree with your own tools, and nothing leaves your Mac until you run `submit`.

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

`setup tart` and `setup github` do those two parts by themselves. `setup` finds the remote for `macports/macports-ports` and puts branch worktrees in `~/Source/macports-branches`, unless you say otherwise. Your own checkout is left alone: each branch gets a sparse worktree of its own, holding only `_resources` and the ports it changes.

## An update, start to finish

```sh
dockhand update jq --new          # a branch from fresh master, jq moved to its newest release
cd "$(dockhand path jq-…)"        # the branch's worktree, if you want to look or edit
dockhand check                    # build it in a clean VM of this Mac's macOS
dockhand tidy                     # one commit, "jq: update to 1.8.1"
dockhand submit                   # push to your fork and open the pull request
```

Each step shows what it will do before it does it, and `--plan` on any of them shows the plan and changes nothing. `update jq --new --submit` runs the whole sequence, previewing each step, and `bump jq` runs it asking nothing, stopping wherever you should look. If the check fails, the branch stays as it is. `dockhand logs check-12 --port jq` prints the log, and you fix the Portfile in the worktree and run `check` again. Checks build the files as they are on disk, committed or not, so there is nothing to commit first.

`status` shows every open branch: its ports, its edits, its latest check, and its pull request, under a list of what needs you, each with the command that moves it forward. Inside a branch's worktree, `status` shows that branch in detail.

## Where checks build

```sh
dockhand setup                    # what is set up, and what is missing
dockhand check --on sequoia --on tahoe      # both releases, and both must pass
```

- **tart**, the default: a fresh clone of dockhand's own image for each macOS release, deleted afterwards. It needs an Apple silicon Mac with [Tart](https://tart.run) (`sudo port install tart`). The first image is this Mac's release; `setup tart sequoia` adds another. Ports that need the full Xcode build only in an Xcode image, which `setup tart --xcode ~/Downloads/Xcode_26.xip` adds. Without one, those ports are reported as not built, and never as failed.
- **github**: MacPorts' own CI workflow, run in your fork's GitHub Actions. It needs the GitHub login and the workflow enabled in your fork.
- **command**: your own script, for a build box or a VM you manage. See [the command provider](docs/command-provider.md).

The pull request's *Tested on* section says what each environment was, down to the macOS build and Xcode version the guest reported, and gives the ID of each run, which `dockhand logs` looks up.

## Keeping up with your ports

```sh
dockhand outdated --mine                  # which of your ports have newer releases
dockhand update --outdated --mine --check # one branch and one commit each, and a check of each queued
dockhand serve --drain                    # run the queued checks, then exit
dockhand submit --passing                 # go through the ones that passed
```

`--mine` means the ports whose `maintainers` line names you, from `maintainer` in `~/.dockhand/config.toml`. Left running, `dockhand serve` runs the checks you queue, such as with `check -d`, looks for new releases of your ports each day, and follows your pull requests' reviews and CI. It posts macOS notifications as checks finish and pull requests change, and `serve --install` makes it a launchd agent that starts at login. It opens no pull requests unless you ask it to, with `serve.submit_passing`, and then within a daily limit.

## Other kinds of change

| Command | What it does |
| --- | --- |
| `create <url>` | writes a new port's first Portfile from its GitHub project, marking what it guessed |
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

This release supports **updating ports whose source is on GitHub and that build with Go or Rust**, one port at a time and in batches: `update`, `check`, `tidy`, `submit`, `bump`, and `update --outdated`. Every other Portfile shape either works or refuses with a reason; dockhand never makes an edit it can't stand behind. Shapes outside that scope, which it plans with care or refuses:

- ports on another forge, such as GitLab or Codeberg, and ports MacPorts fetches with Git;
- Python ports, and ports whose subports share a revision or a release;
- C libraries with `--revbump-dependents`, and ports checked variant by variant.

Read what `--plan` shows for these before you go on. Two commands are **experimental** until dockhand runs another person's Portfile apart from your own: `adopt --pr` and `review` evaluate a pull request's Portfiles with MacPorts on your Mac, and say so each time.

Building in Tart needs an Apple silicon Mac. An Intel Mac, like any other, prepares changes here and checks them on GitHub Actions, `--on github`, which runs MacPorts' own workflow in your fork.

## Known issues

What the release-candidate test finds that's rough but harms nothing is listed here, with its workaround.

- A check builds on this Mac's release unless told otherwise, and MacPorts CI also builds on macOS 14 and 15. `submit` and the pull request say which releases no check covered; `--on ci` checks on all three.
- A Go port whose module moved hosts, as pomo moved from GitHub to Codeberg, is planned without naming the move. Compare the new release's `go.mod` module path with the Portfile's before `tidy`.
- Subports that share a Portfile but differ beyond what dockhand reads, such as py-lmdb's, can get a planned edit that needs a person's look. Read `dockhand diff` before `tidy`.
- A revision that changes only a patch's contents, keeping its name, reads as fetching the base's source, so the upstream assessment compares nothing for it, that patch included. A check still applies it, and fails where it doesn't apply, so check after editing a patch.
- `serve` has printed a check's "passed" line twice (check-130, field testing, 2026-10-02). It hasn't recurred, and it changes nothing but the output.

## Requirements

- **macOS on Apple silicon** to build in Tart. Any Mac can prepare changes and check them on GitHub Actions or your own script.
- **MacPorts.** Dockhand reads Portfiles through MacPorts' own Tcl interpreter, so it sees what `port` sees.
- **Git 2.40 or newer, and a clone of your fork** of `macports/macports-ports` that Git can push to. `setup` checks the Git it runs: `git` on `PATH`, or `$GIT_BIN`. Dockhand finds the upstream remote whatever it is called, and your fork by your GitHub login.
- **A GitHub login** for `submit`: `dockhand setup github`, `GH_TOKEN` or `GITHUB_TOKEN`, or the GitHub CLI's.
- **`go2port` or `cargo2port`**, from MacPorts, only for Go and Rust ports whose Portfile lists its dependencies.

### Installing

Build from source with Go 1.27.1 or newer. The dependencies are vendored, so the build needs no network:

```sh
git clone https://github.com/herbygillot/dockhand.git
cd dockhand
make build
mkdir -p "$HOME/.local/bin" && install -m 755 dockhand "$HOME/.local/bin/dockhand"
```

`dockhand --version` shows which build you have. For v2, build its tag beside this checkout:

```sh
git worktree add ../dockhand-v2 v2-final
make -C ../dockhand-v2 build BINARY="$HOME/.local/bin/dockhand-v2"
```

## More

[`docs/usage.md`](docs/usage.md) is the guide to every command, setting, and provider. [`docs/design-v3.md`](docs/design-v3.md) is the design and its reasons, [`docs/architecture.md`](docs/architecture.md) maps it onto the code, and [`docs/roadmap.md`](docs/roadmap.md) says what comes next. `dockhand <command> --help` is the reference for each command, and `dockhand config` shows every setting in effect.

Dockhand is developed at [github.com/herbygillot/dockhand](https://github.com/herbygillot/dockhand) and licensed under the [MIT License](LICENSE).
