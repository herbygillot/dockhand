> [!WARNING]
> ⚠️ **Pre-release software undergoing rapid change.** Commands, behavior, and stored data may change or break without notice. There are no stability or compatibility guarantees. Use Dockhand at your own risk.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/logo-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="images/logo-light.png">
    <img alt="dockhand logo" src="images/logo-light.png" width="480">
  </picture>
</p>

# dockhand

**From upstream release to submitted port update.**

Dockhand helps [MacPorts](https://www.macports.org) contributors prepare port updates, verify them in a clean macOS virtual machine, and submit pull requests. You work through your own fork of the ports tree; no commit access to the MacPorts repository is needed.

It handles the repetitive parts—finding an upstream version, updating supported Portfiles and checksums, preparing a branch, and checking the build—so you can concentrate on the change itself. You can preview and review each step, or take an update all the way to a pull request with one command.

## Build

Build from source with Go 1.27.1 or newer:

```sh
git clone https://github.com/herbygillot/dockhand.git
cd dockhand
make build
```

This produces `./dockhand`. To use the commands below from your ports checkout, put the binary on your `PATH`, for example:

```sh
mkdir -p "$HOME/.local/bin"
install -m 755 dockhand "$HOME/.local/bin/dockhand"
export PATH="$HOME/.local/bin:$PATH"
```

Running Dockhand requires macOS, Git, and a local MacPorts installation. VM verification also requires [Tart](https://tart.run) and an Apple silicon Mac.

## Prepare your workspace

Fork `macports/macports-ports` on GitHub, then clone **your fork** and add the upstream repository:

```sh
git clone git@github.com:YOUR_USERNAME/macports-ports.git
cd macports-ports
git remote add upstream https://github.com/macports/macports-ports.git
```

Run the following examples from this checkout. To work from elsewhere, pass `--tree /path/to/macports-ports` (or set `MACPORTS_TREE`).

For pull requests, Dockhand can use an existing GitHub CLI login (`gh auth login`), a `GH_TOKEN` or `GITHUB_TOKEN` credential, or its own configured browser login. Git must also be able to push to your fork.

Prepare a verification image once:

```sh
dockhand setup
```

Setup provisions a macOS image with development tools and MacPorts. It defaults to the host release; use `dockhand setup --os sonoma` to prepare another supported macOS release. Each verification runs in a disposable clone. For ports that require full Xcode, provide a downloaded Xcode archive with `dockhand setup --xcode /path/to/Xcode.xip`.

You can also verify through GitHub Actions on your personal MacPorts fork, with its existing Actions workflow enabled:

```sh
dockhand bump croc --provider github --publish --wait
```

This pushes the update to your fork (the `origin` remote by default) and opens the upstream PR after the workflow passes. The workflow controls its macOS matrix and test policy; a green run does not guarantee every port test passed.

## Preview an update

Start by looking at the proposed change:

```sh
dockhand bump jq --diff
```

Dockhand selects an eligible upstream version and prints the patch without creating a branch. You can also name a version:

```sh
dockhand bump jq 1.8.1 --diff
```

An upstream tag prefix such as `v` can be included; if omitted, Dockhand uses the port's existing prefix convention. Automatic updates currently cover supported GitHub and GitLab source conventions. Some Portfiles still need manual edits; Dockhand reports when it cannot prepare an update.

For ports with generated Go or Rust dependency blocks, Dockhand uses the optional host tools `go2port` (`go.vendors`) and `cargo2port` (`cargo.crates`, including supported GitHub Git dependencies). Install the needed helper with MacPorts, for example `sudo port install go2port` or `sudo port install cargo2port`. `dockhand setup` reports whether they are available; unrelated updates do not require them. Use `--go2port` / `GO2PORT_BIN` or `--cargo2port` / `CARGO2PORT_BIN` to choose an executable. If a block contains overrides it cannot safely regenerate, Dockhand asks you to prepare that change manually.

## Prepare and verify

```sh
dockhand bump jq --wait
```

Dockhand starts from freshly fetched **MacPorts `master`**, creates an update branch, and verifies it using the image prepared by `setup`. It prints the branch name for inspection. Your current checkout stays in place.

Use `--trace` instead of `--wait` to follow the build logs. Available dependency binaries are used by default. To prepare only the branch for manual work, use `--no-verify`.

When you are ready, preview publication and then open the pull request:

```sh
dockhand publish --branch <prepared-branch> --dry-run
dockhand publish --branch <prepared-branch> --wait
```

Dockhand pushes to your fork and opens the PR against upstream. Publication requires passing verification for the committed contents.

## Go from release to pull request

To prepare, verify, and submit an update in one run:

```sh
dockhand bump jq --publish --wait
```

A failed verification preserves the prepared branch for investigation. An automatic bump that finds the port already current finishes without opening a PR.

## Make your own edits

The prepared branch is an ordinary Git branch. Switch to it, edit the Portfile or patches, and amend the contribution commit before verifying again:

```sh
git switch <prepared-branch>
# Make your edits, then stage the changed files.
git add <changed-files>
git commit --amend --no-edit
dockhand verify jq --trace
dockhand publish --wait
```

Current publication support expects one contribution commit confined to one port directory. You can also use `verify` on a branch you created yourself. It captures tracked working-tree edits; stage new files to include them. `--branch <name>` verifies committed contents instead.

## Pick up where you left off

`--wait` keeps Dockhand attached until the requested work finishes. Without it, a build command waits for an available VM slot and returns once the build is admitted. Ctrl-C detaches; it does not cancel accepted work.

```sh
dockhand status
dockhand wait <job-id> --trace
dockhand cancel <job-id> --wait
```

`status` reads recorded progress; `wait` resumes processing and follows the job through completion. Use `dockhand --help` or a command's `--help` for more options. Detailed project logs and further documentation are available in `docs/`.

Licensed under the [MIT License](LICENSE).
