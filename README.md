> [!NOTE]
> **Early software; the command surface is still settling.** Your stored data is safe: the state database is migrated forward by each newer Dockhand, never discarded, and `dockhand db backup` keeps a copy before any migration. Commands, flags, output, and the workflow itself may still change between releases, so scripts built on them should expect to be revisited.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/logo-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="images/logo-light.png">
    <img alt="dockhand logo" src="images/logo-light.png" width="480">
  </picture>
</p>

# dockhand

**From upstream release to submitted port update.**

Dockhand is for people who keep [MacPorts](https://www.macports.org) ports up to date and contribute the updates as pull requests. Given a port, it finds the newest upstream release, edits the Portfile and checksums, builds the port in a clean macOS virtual machine or on GitHub Actions, and opens the pull request from your fork. You look at the result at every step, and nothing goes out until it has been built or you have said it should.

Two things a MacPorts contributor will want to know first:

- **It works entirely through your own fork** of `macports/macports-ports`. The pull request it opens is the same kind you would open by hand, and nothing is ever pushed to the MacPorts repository directly. You do not need commit access.
- **It builds the way a MacPorts pull request is built.** Lint, build, and install decide the verdict. The port's tests run and their result is reported in the pull request, but a failing test suite does not fail the build, the same rule the MacPorts workflow applies; `--tests required` makes it decisive.

When a Portfile does something Dockhand does not understand, or a build fails, or upstream looks wrong, it stops, keeps what it has done, and tells you why. It never guesses.

## The thirty-second version

```sh
dockhand auth login      # once: a browser login to GitHub
cd ~/Source/macports-ports
dockhand bump jq
```

The last command fetches the current MacPorts `master`, creates a branch named like `dockhand/bump/jq-…`, edits the port to the newest release, builds it, pushes the branch to your fork, and opens the pull request. It stays in the foreground and prints each milestone: the version move, the branch, where it is building, the verdict, and the pull request URL. Your checkout is not touched. If the port is already current, it says so and stops. If the build fails, the branch is kept and no pull request is opened.

That is the whole tool for most updates. The rest of this page is what to do around it.

## Where builds happen

Dockhand needs somewhere to build. You have two choices, and it picks whichever is available.

- **A local virtual machine**, on an Apple silicon Mac with [Tart](https://tart.run) installed. Run `dockhand setup` once to prepare a clean macOS image with MacPorts in it; it takes a while the first time. `dockhand setup --xcode /path/to/Xcode.xip` prepares a second image for ports that need full Xcode, and Dockhand chooses the right one per port. This is the faster and more detailed option: each phase gets its own verdict and you can follow the log with `--trace`.
- **GitHub Actions in your fork**, when there is no Tart image. This uses the MacPorts workflow your fork already has, so make sure that workflow is enabled under the fork's Actions settings. It needs the GitHub login above and a fork Dockhand can find among your checkout's remotes; without those, a bump says so before doing any work.

`--provider tart` or `--provider github` overrides the choice.

## The everyday loop

```sh
dockhand outdated --maintainer you@example.org   # which of my ports have a newer release
dockhand bump jq --diff                          # what would change, without touching anything
dockhand bump jq                                 # do it: branch, build, pull request
dockhand status                                  # everything I have open, as a live table
dockhand refresh jq                              # record the PR's fate after review
```

`outdated` reads your checkout and asks each port's upstream, or its livecheck, whether there is something newer. `bump jq 1.8.1` names the version yourself; `bump-revision jq --reason "…"` and `refresh-checksums jq` do the other two kinds of update, with the same flags. `status` shows one row per port with its phase, state, and what comes next; on a terminal it is live, processes your pending work while open, and its keys run the other commands on the selected row. After a pull request is merged or closed, `refresh` records it and cleans up the branches so the next bump starts fresh.

Every accepted job is durable. Close the terminal and `dockhand wait jq` picks the same job back up; Ctrl-C detaches without canceling anything, and `dockhand cancel jq` cancels on purpose.

## When something goes wrong

A failed build keeps the prepared branch. Switch to it, look at the log the verdict line points at, fix the Portfile or patches, stage the change, and let Dockhand take it from there:

```sh
dockhand amend      # capture the staged fix as the contribution's new commit, rebuild, update the PR
dockhand rebase     # reapply the contribution onto current master when it has moved on
dockhand abandon jq # stop pursuing this update; the branch and its history are kept
```

`amend --diff` and `rebase --diff` show what would change first. Both verify and update the pull request the way `bump` does, and take the same stop-early flags below.

Dockhand's branches are ordinary Git branches, and it will build and publish a branch you made yourself: keep the contribution to one commit in one port directory, then `dockhand verify` and `dockhand publish` it.

## Stopping early, or skipping the build

One `--to` and one modifier cover every change command:

| Flags | What happens |
| --- | --- |
| none | prepare, build, open the pull request |
| `--to verified` | prepare and build; look before you publish |
| `--to branch` | prepare the branch and stop |
| `--unverified` | prepare and open the pull request without building; the PR says so |

`--detach` submits the work and returns as soon as the build is admitted; `wait` finishes it. `--diff` previews without creating anything. A branch left at `--to verified` is published later with `dockhand publish jq`, and `publish --dry-run` shows the full pull request body first.

## Requirements

- **macOS.** Local builds need an Apple silicon Mac with Tart; the prepared images cover macOS Monterey through Golden Gate. Any other Mac can still prepare updates and build them on GitHub Actions.
- **A local MacPorts installation.** Dockhand reads Portfiles through MacPorts' own Tcl interpreter, so it sees exactly what `port` sees.
- **Git, and a clone of your fork** of `macports/macports-ports` that Git can push to. Dockhand recognizes `macports/macports-ports` as upstream whatever the remote is called, and finds your fork by your GitHub login; `--remote` is only for ambiguous layouts.
- **A GitHub credential**: `dockhand auth login`, an existing `gh auth login`, or `GH_TOKEN`.
- **`go2port` or `cargo2port`**, from MacPorts, only for ports whose Portfile carries a generated Go or Rust dependency block.

### Installing

Build from source with Go 1.27.1 or newer; the dependencies are vendored, so the build needs no network:

```sh
git clone https://github.com/herbygillot/dockhand.git
cd dockhand
make build
mkdir -p "$HOME/.local/bin" && install -m 755 dockhand "$HOME/.local/bin/dockhand"
```

`dockhand --version` shows which build you have.

## More

Dockhand keeps its records in one SQLite database, `~/.dockhand/state.db` unless `--db` or `DOCKHAND_DB` says otherwise. `dockhand gc` prunes old build environments and logs, and `dockhand db backup` copies the database.

[`docs/usage.md`](docs/usage.md) covers every command and option, which Portfile shapes can be bumped automatically, credential precedence, and what each build actually runs. [`docs/github-verification.md`](docs/github-verification.md) covers building in your fork, and [`docs/operations.md`](docs/operations.md) the database, backups, and retention. The rest of [`docs/`](docs/) is the design. `review` appears in `--help` but is not implemented yet.

Dockhand is developed at [github.com/herbygillot/dockhand](https://github.com/herbygillot/dockhand) and licensed under the [MIT License](LICENSE).
