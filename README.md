> [!WARNING]
> **Pre-release software under rapid development.** Commands, behavior, and stored data can change or break without notice. There are no stability or compatibility guarantees yet.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/logo-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="images/logo-light.png">
    <img alt="dockhand logo" src="images/logo-light.png" width="480">
  </picture>
</p>

# dockhand

**From upstream release to submitted port update.**

## Description

Dockhand is a command-line tool for people who keep [MacPorts](https://www.macports.org) ports up to date and contribute those updates through GitHub pull requests. It does the repetitive part of a port update for you: it finds the new upstream version, edits the Portfile and checksums, builds the port in a clean macOS virtual machine or on GitHub Actions, and opens the pull request from your fork. You look at the result at every step, and you decide when it goes out.

You do not need commit access to MacPorts. Dockhand works entirely through your own fork of `macports/macports-ports`. The pull request it opens is the same kind you would open by hand, and nothing is ever pushed to the MacPorts repository directly.

You can take an update one step at a time, previewing the diff, preparing a branch, building it, and publishing it as separate commands. Or you can take a port from its latest release to a submitted pull request with one command. Either way, Dockhand never guesses. When a Portfile does not follow a convention it understands, or a build fails, or something upstream looks wrong, it stops, keeps what it has done so far, and tells you why.

## Requirements

- **A Mac running macOS.** Building ports in local virtual machines needs an Apple silicon Mac with [Tart](https://tart.run) installed; the prepared images cover macOS Monterey through Tahoe. On any other Mac you can still prepare updates and build them on GitHub Actions instead.
- **Go 1.27.1 or newer**, to build Dockhand from source. There are no binary releases yet.
- **Git.**
- **A local MacPorts installation.** Dockhand reads Portfiles through MacPorts' own Tcl interpreter, so it sees exactly what `port` sees, including computed versions and PortGroup effects.
- **A GitHub account with a fork of `macports/macports-ports`**, cloned to your machine, with Git able to push to that fork.
- **A GitHub credential for Dockhand.** Any one of these works: an existing GitHub CLI login (`gh auth login`), a `GH_TOKEN` or `GITHUB_TOKEN` environment variable, or Dockhand's own browser login (`dockhand auth login`, available when Dockhand was built with a registered OAuth client ID).

Optional, depending on what you work on:

- **An Xcode archive** (`Xcode.xip`) for ports that need full Xcode rather than the Command Line Tools. Dockhand builds a separate VM image from it.
- **`go2port` and `cargo2port`** for ports with generated Go or Rust dependency blocks. Install them with MacPorts; Dockhand only needs the one a given port uses.

## Features

**Preparing updates**

- Version bumps that find the newest stable upstream release from GitHub or GitLab tags or releases, following the port's own livecheck and tag conventions, or use a version you name.
- Revision bumps.
- Literal `version`, `github.setup`, `gitlab.setup`, and GitHub-backed `go.setup` sources, with checksums recomputed from the real archives, including ports with several archives or named checksums.
- Regenerated `go.vendors`, `cargo.crates`, and `cargo.crates_github` blocks, checked against the upstream module and lockfile so a helper that silently drops a dependency is caught.
- A preview mode that prints the exact diff without creating a branch or touching your checkout.
- Every update starts from freshly fetched MacPorts `master`, on a new branch, with your working copy left alone.

**Building and checking**

- Clean builds in a disposable virtual machine cloned from an image you prepare once: lint, build, declared tests, and install, with the build log available live.
- Builds on GitHub Actions in your fork when you have no Tart, using the MacPorts workflow that already lives there.
- Automatic choice between the two for bumps, or an explicit `--provider`.
- Reuse of a passing result when the same source tree and build settings are checked again, so committing verified edits does not cost a second build.
- Checks of your own hand-made branches and even uncommitted working-tree edits, not only branches Dockhand prepared.

**Publishing**

- Pull requests opened from your fork against MacPorts, with the commit message as the description and a body that reports the build environment and a review checklist based on what actually ran.
- Publication only for committed contents with a passing build on record. Dockhand refuses to publish anything it has not seen pass.
- Safe recovery when GitHub does not answer: an uncertain pull-request request is checked, never blindly repeated.

**Working style**

- Work is durable. Close the terminal, come back later, and pick up the same job with `wait`.
- Ctrl-C detaches from a running build without canceling it; `cancel` cancels on purpose.
- A failed build keeps the prepared branch so you can fix it and check again.
- `status` shows what Dockhand has recorded without starting anything.
- `--json` on any command for scripting.

## Using It

### Build and install

```sh
git clone https://github.com/herbygillot/dockhand.git
cd dockhand
make build
```

This produces `./dockhand`. Put it somewhere on your `PATH`:

```sh
mkdir -p "$HOME/.local/bin"
install -m 755 dockhand "$HOME/.local/bin/dockhand"
export PATH="$HOME/.local/bin:$PATH"
```

### Set up your fork

Fork `macports/macports-ports` on GitHub if you have not already, then clone **your fork** and add MacPorts as `upstream`:

```sh
git clone git@github.com:YOUR_USERNAME/macports-ports.git
cd macports-ports
git remote add upstream https://github.com/macports/macports-ports.git
```

Run the commands below from inside this checkout. To run them from somewhere else, add `--tree /path/to/macports-ports` or set `MACPORTS_TREE`. Dockhand pushes to `origin` and targets `upstream` by default; `--remote` and `--upstream` change that.

### Sign in to GitHub

If you already use the GitHub CLI, `gh auth login` is enough. Otherwise export `GH_TOKEN` or `GITHUB_TOKEN`, or use Dockhand's browser login when your build includes an OAuth client ID:

```sh
dockhand auth login
dockhand auth status
```

`auth status` tells you which credential Dockhand will use and which account it belongs to. `auth logout` removes only Dockhand's own saved credential.

### Prepare a build VM (optional)

To build ports locally, prepare a VM image once. This pulls a vanilla macOS image, installs the Command Line Tools and MacPorts into it, and checks the result:

```sh
dockhand setup
```

It takes a while the first time. Use `dockhand setup --check` to validate an existing image, `dockhand setup --os sonoma` to prepare a different macOS release, and `dockhand setup --xcode /path/to/Xcode.xip` to build a second image for ports that need full Xcode. Dockhand picks the right image for each port automatically.

If you skip this step, Dockhand builds on GitHub Actions in your fork. Make sure the fork's existing `main.yml` workflow is enabled under its Actions settings.

### Preview an update

Start by looking at what Dockhand would change:

```sh
dockhand bump jq --diff
```

It picks the newest eligible upstream version and prints the patch. Nothing is written. To choose the version yourself:

```sh
dockhand bump jq 1.8.1 --diff
```

Include the upstream tag prefix if you like (`v1.8.1`); if you leave it off, Dockhand follows the port's existing convention. A revision bump works the same way with `dockhand bump-revision jq --diff`.

If the port's Portfile does something Dockhand does not understand, the preview says so instead of producing a guess. Those ports still need a manual edit, and you can hand that edit back to Dockhand for building and publishing, as described below.

### Prepare and check an update

```sh
dockhand bump jq --wait
```

Dockhand fetches the current MacPorts `master`, creates an update branch named like `dockhand/bump/jq-...`, and builds it: in a Tart VM if you prepared an image, otherwise on GitHub Actions. It prints the branch name so you can inspect it. Your current checkout is not touched.

- `--trace` instead of `--wait` streams the build log as it runs.
- `--no-verify` prepares the branch and stops, for updates you want to finish by hand.
- `--provider tart` or `--provider github` overrides the automatic choice.
- `--from-source` builds dependencies from source instead of using binary archives.

A failed build keeps the branch. Switch to it, look at the log, fix what needs fixing, and check it again with `verify`.

### Open the pull request

When the branch has a passing build, preview the publication and then open the pull request:

```sh
dockhand publish --branch dockhand/bump/jq-... --dry-run
dockhand publish --branch dockhand/bump/jq-... --wait
```

The dry run shows the full pull-request body without pushing anything. The real run pushes the branch to your fork and opens the pull request against MacPorts. Publishing requires a passing build for exactly the committed contents; if you changed the branch since it was built, Dockhand asks you to verify it again first.

### Do it all in one command

```sh
dockhand bump jq --publish --wait
```

This prepares the update, builds it, and opens the pull request. If the port is already at the newest version, the command finishes with nothing to do. If the build fails, the branch is kept and no pull request is opened.

### Edit by hand

Dockhand's branches are ordinary Git branches, and Dockhand is happy to build and publish branches you made yourself. Edit the Portfile or patches, keep the contribution as one commit confined to one port directory, then check and publish:

```sh
git switch dockhand/bump/jq-...
# edit, then stage the changed files
git add <changed-files>
git commit --amend --no-edit
dockhand verify jq --trace
dockhand publish --wait
```

`verify` checks the working tree by default, including staged edits, so you can build before you even commit; new files must be staged to be included. Add `--branch <name>` to check a branch's committed contents instead. Without a Tart image, add `--provider github`, which pushes the branch to your fork for the workflow to build.

### Follow, resume, or cancel

Everything Dockhand accepts is recorded, so you can leave and come back:

```sh
dockhand status
dockhand status --active
dockhand wait <job-id> --trace
dockhand cancel <job-id> --wait
```

`status` reads what is recorded and starts nothing. `wait` resumes a job and follows it to the end. `wait` and `cancel` also accept `--branch <name>`, or no selector at all when you are on the branch in question. Ctrl-C detaches from a running command; the build keeps going, and `cancel` is how you stop it. `dockhand start` keeps working through every pending job for the checkout until you interrupt it.

Commands exit with 0 on success, 2 when the requested build failed, 3 when something needs your attention, 130 when interrupted or canceled, and 1 for any other error.

### Housekeeping

Dockhand keeps its records in one SQLite database, `~/.dockhand/state.db` by default (`--db` selects another). VMs from failed builds are kept for a week so you can inspect them, then released:

```sh
dockhand gc --dry-run
dockhand gc
dockhand db backup ~/Backups/dockhand.db
dockhand db check
```

If a newer Dockhand reports that the database needs upgrading, run `dockhand db migrate`.

## Additional Info

The `docs/` directory has the detailed material:

- [`docs/usage.md`](docs/usage.md): every command and option, credential precedence, remote selection, image management, and what each build actually runs.
- [`docs/github-verification.md`](docs/github-verification.md): building on GitHub Actions in your fork, what a green workflow does and does not prove, and how to recover when a run goes missing.
- [`docs/dependency-preparation.md`](docs/dependency-preparation.md): how Go and Rust dependency blocks are regenerated and checked, and which layouts still need manual preparation.
- [`docs/operations.md`](docs/operations.md): the state database, backups, restoring from one, and retention of VMs and logs.
- [`docs/development.md`](docs/development.md): building and testing Dockhand itself.
- [`docs/architecture.md`](docs/architecture.md), [`docs/principles.md`](docs/principles.md), [`docs/components.md`](docs/components.md), [`docs/cli-design.md`](docs/cli-design.md), and [`docs/state.md`](docs/state.md): the design. [`docs/roadmap.md`](docs/roadmap.md) is the current queue, and `docs/activity/` holds a dated report for every change that has landed.

A few things worth knowing up front:

- Automatic version discovery currently covers sources hosted on GitHub or GitLab that follow the standard PortGroup conventions. Other ports can still be bumped to a version you name, or edited by hand and then built and published with Dockhand.
- `refresh-checksums` and `review` appear in `--help` but are not implemented yet.
- Build results, VM images, and logs live outside the database: VMs under Tart's home directory, logs and artifacts next to the database under `~/.dockhand/`.
- A successful GitHub Actions workflow is recorded as a pass under the workflow's own rules, which may tolerate individual port test failures. A Tart build reports lint, build, tests, and install separately.

Dockhand is developed at [github.com/herbygillot/dockhand](https://github.com/herbygillot/dockhand) and licensed under the [MIT License](LICENSE).
