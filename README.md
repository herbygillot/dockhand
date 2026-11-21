<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/logo-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="images/logo-light.png">
    <img alt="dockhand logo" src="images/logo-light.png" width="480">
  </picture>
</p>

**Prepare, test, and submit changes to MacPorts ports.**

Dockhand takes a port from a new upstream release to a pull request on [macports/macports-ports](https://github.com/macports/macports-ports): it finds the release, updates the Portfile and checksums, builds the result in a clean macOS environment, and opens the pull request from your fork. You don't need commit access to MacPorts.

> These examples use the v0.8.0 command interface. Run `dockhand --version` to check your build and `dockhand <command> --help` for its options.

## Quick start

You need MacPorts, Git 2.40 or later, and a [fork of macports/macports-ports](https://github.com/macports/macports-ports/fork).

**1. Install Dockhand and clone your fork.** Replace `YOUR-USERNAME` with your GitHub username, or skip the clone and `cd` into the ports checkout you already have.

```sh
sudo port install dockhand
git clone https://github.com/YOUR-USERNAME/macports-ports.git
cd macports-ports
git remote add upstream https://github.com/macports/macports-ports.git
```

**2. Set up.**

```sh
dockhand setup
```

`setup` registers the checkout, signs you in to GitHub, and offers to prepare a [Tart](https://tart.run) VM image to build in. The image downloads macOS and can use up to 60 GB; on Apple silicon, `sudo port install tart` first. Without Tart, or on an Intel Mac, enable Actions in your fork and add `--on github` to the commands below. Git must also be able to push to your fork without a password prompt, through SSH or a credential helper.

**3. Update a port.** Pick one with a newer upstream release; these examples use `ripgrep`.

```sh
dockhand bump ripgrep --plan
dockhand bump ripgrep
```

`--plan` shows what will happen and changes nothing. `bump` then updates the port, builds it, and **opens a pull request on MacPorts without asking**. It stops and keeps the branch for you instead when something needs a person: a failed build, a major-version change, another open pull request for the port, or an upstream change a build can't judge. `dockhand status` shows where things stand and the next command to run.

## Look before you submit

`ship` does what `bump` does, but shows its plan and asks once before it submits. It can also stop earlier:

```sh
dockhand ship ripgrep
dockhand ship ripgrep --to commit   # update and commit only
dockhand ship ripgrep --to check    # update, commit, and build
```

Run `ship` or `bump` again to resume where it stopped.

## Work step by step

Each change is an ordinary Git branch with its own worktree, so you can edit with your usual tools and run each stage yourself:

```sh
dockhand update ripgrep
cd "$(dockhand path -p ripgrep)"
dockhand diff
dockhand check
dockhand submit --plan
dockhand submit
```

`update` starts a branch for the port at its newest release. `check` builds the files as they are on disk, and `submit` commits them, pushes to your fork, and opens or updates the pull request. To shape the commits yourself first, run `dockhand tidy` before `submit`. Add `--plan` to any of these to preview it.

## Learn more

Dockhand carries its own guide, written for the version you installed:

```sh
dockhand usage              # setup and the path to a pull request
dockhand usage checks       # where checks build: Tart, GitHub Actions, a local prefix
dockhand usage recovery     # when something stops, and how to resume
dockhand usage batches      # the ports you maintain, many at once
dockhand usage unattended   # bump, serve, and what stops for a person
```

`dockhand usage --help` lists every topic, and `dockhand <command> --help` covers each command. The repository has the same pages for the development branch, [docs/usage.md](docs/usage.md), with [more port work](docs/usage.md#more-port-work), [what Dockhand covers](docs/usage.md#what-dockhand-covers), and [install details](docs/usage.md#install-details).

Dockhand is built for routine maintenance: version updates from GitHub releases and GitLab tags, with Go and Rust ports the most exercised. Ports it can't update yet, such as Python ports, get a plan or a clear refusal with the manual steps rather than a wrong edit. Each release's known issues are in its [release notes](https://github.com/herbygillot/dockhand/releases).

<details>
<summary>Build Dockhand from source</summary>

Install Go 1.27.1 or later, then:

```sh
git clone --branch v0.8.0 https://github.com/herbygillot/dockhand.git
cd dockhand
make build
mkdir -p "$HOME/.local/bin"
install -m 755 dockhand "$HOME/.local/bin/dockhand"
```

Make sure `~/.local/bin` is on your `PATH`. Dependencies are vendored; `make build` does not need to download Go modules.

</details>

## Help and contributing

`dockhand <command> --help` covers each command, and `dockhand explain` says why a check, finding, or branch ended up as it did. Report bugs and propose improvements on [the issues page](https://github.com/herbygillot/dockhand/issues), with your `dockhand --version`, the command you ran, and the error or build log. For MacPorts' own practices, see its [contribution workflow](https://guide.macports.org/#project.github) and [commit-message guidelines](https://trac.macports.org/wiki/CommitMessages). To work on Dockhand itself, run `make build` and `make test`.

## License

[MIT](https://github.com/herbygillot/dockhand/blob/main/LICENSE).
