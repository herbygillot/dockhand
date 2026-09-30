# Using dockhand

This is the guide to dockhand v3: how its commands fit together, and the choices each one leaves you. `dockhand <command> --help` is the reference for any one command, and `dockhand config` shows every setting in effect. [Design v3](design-v3.md) gives the reasons. The v2 guide is kept in [`v2/usage.md`](v2/usage.md), for the tag `v2-final`.

## Setting up

In your clone of `macports/macports-ports`, the one with your fork as a remote:

```sh
dockhand init
```

`init` first checks the Git dockhand runs, `git` on `PATH` or `$GIT_BIN`, and refuses one older than 2.40, which `rebase` needs; macOS's own Git is new enough on current releases, and `sudo port install git` gets one otherwise. It then registers the checkout and finds its remote for `macports/macports-ports`, whatever it is called. It also chooses where branch worktrees go: `~/Source/macports-branches`, wherever the clone is, unless `--worktrees <dir>` or the configuration's `worktrees` names another. Running it again is safe. It needs no GitHub login and no build setup; those come when a command needs them.

Every command works on one ports checkout: `--tree` (`-t`), else `$MACPORTS_TREE`, else the directory you are in. Inside a branch's worktree, that is the checkout it belongs to, and the branch checked out there is the one a command means, `$MACPORTS_TREE` or not.

### Somewhere to build

`dockhand providers` lists the places a check can build and whether each is ready. The usual first step is a Tart image of this Mac's macOS:

```sh
sudo port install tart
dockhand providers setup tart
```

See [Providers](#providers) for the others and for more releases.

### A GitHub login

`submit`, the github provider, and `status --refresh` need to act as you on GitHub. Dockhand takes the first of these it finds:

1. `GH_TOKEN`, then `GITHUB_TOKEN`;
2. its own login, from `dockhand auth login`, kept in the macOS Keychain. The login is a one-time code in the browser, asking for the `public_repo` scope;
3. the GitHub CLI's login, through `gh auth token`.

`dockhand auth status` says which account that is, and `dockhand auth logout` removes dockhand's own login.

### The configuration file

Settings live in `~/.dockhand/config.toml`, or the file `$DOCKHAND_CONFIG` names. Nothing in it is required, and an unknown key is refused by name. A flag comes before the environment, and the environment before the file. [All the settings](#settings) are listed at the end of this page. Two are worth setting early:

```toml
maintainer = "{@you example.org:you} openmaintainer"   # your maintainers line, for --mine and create

[check]
on = ["tart:sequoia,tahoe"]   # where checks build unless --on says otherwise
```

## Branches

All work happens on branches. A branch is an ordinary Git branch of the ports tree, named `dockhand/<name>`, with a base on MacPorts' master and at most one pull request. Dockhand keeps a record of each one it tracks.

- **`start <name>`** creates `dockhand/<name>` from master, fetched just now, in a worktree of its own. `--here` creates it in this checkout instead, which must have no uncommitted changes to tracked files.
- **`update`, `checksums`, `create`, and `edit` with `--new`** start a branch named after the port, such as `dockhand/jq-4f2a`. A version update starts it once there is an edit to make, so a port already current starts nothing. `revbump` starts one by itself unless `--branch` names one or you are in one, since a rebuild has its own reason.
- **`adopt [branch]`** tracks a branch you made yourself, as it stands. Its base is where it leaves master.
- **`adopt --pr <number>`** brings someone's pull request into a branch of its own, `pr-<number>`, to look at and work on. Dockhand assumes no permission to push to their branch: `submit` pushes there only when they let maintainers edit and you have write access, and it never rewrites their description.

A branch's worktree is sparse: it holds `_resources` and the ports the branch changes. `edit <port>` brings another port's directory in. `dockhand path <branch>` prints the worktree's directory, for `cd "$(dockhand path jq-4f2a)"` or an editor. The `dockhand/` prefix is optional wherever a branch is named.

A command finds its branch from `--branch`, else the branch checked out where it runs. An authoring command run on master, with neither, asks on a terminal: it offers the open branches that already change the port, or a new one. Without a terminal it refuses, and names the choices. A branch you made yourself and haven't adopted is refused, with a pointer to `adopt`. A branch you rename with Git keeps its record, recognized by its worktree or its pull request's last push.

## Changing ports

These commands change the branch's working files and commit nothing, unless `update --outdated`, `--submit`, or `bump` asks them to go on. Each remembers what it did, so `tidy` can later write the commit subject a MacPorts reviewer expects, beside your own changes too, while every line it wrote still stands. `--plan` shows the edit and changes nothing.

### update

```sh
dockhand update jq --new           # the newest release, in a new branch
dockhand update jq 1.8.1           # a version you name, in the branch checked out here
dockhand update jq --new --plan    # what would change, from master, starting nothing
```

A plan never starts a branch. With no branch to plan in, none named and none checked out here or changing the port, `update jq --plan` plans on master too. A branch you made with Git, which dockhand doesn't track, counts as none while it doesn't change the port, and the plan says so. Where it does, in commits, edits, or files it adds, a plan on master would leave your changes out, so it refuses, and names `adopt`, which lets the plan read them, and `--new --plan`, which plans without them. Edits to the port on master itself, not committed, are refused the same way. `update jq` on such a branch starts a branch as it would on master, or refuses where the branch changes the port.

`update` names any other open pull request for the port, as submit's preview does, and stops for none. It also names the port's plain-HTTP URLs, its `homepage` and its `master_sites` (never a mirror group, which is MacPorts' own), with whether the `https://` form answers, since MacPorts prefers HTTPS. It leaves them as they are, since changing them is the maintainer's call; `checksums` names them too. It finds the newest release the Portfile's own rules accept, from the project's GitHub or GitLab tags and releases, or its livecheck. It moves the version, resets the revision, and fills in checksums, a variant's own too: git's `+doc` appends git-htmldocs' in its body, and a variant that isn't on by default and fetches an archive of its own is asked about, so none keeps the old version's. For a Go or Rust port whose Portfile lists its dependencies, it regenerates the list with `go2port` or `cargo2port`. `--shared-release` moves every subport sharing the port's release, and `--keep-old-checksums` refreshes legacy md5 or sha1 checksums in place rather than rewriting them as rmd160, sha256, and size.

It then compares the old and new source archives, and reports what a passing build can't catch: a changed license file (one whose copyright lines moved only their years is said with the line, and holds nothing), a changed build file, a new dependency a Python or Node project declares, which is another port the software finds when it runs, a Python requirement that the port providing it, among those the Portfile depends on, doesn't meet at the version the branch has of it, or, for a Go port built in module mode, a Go release its go.mod requires that the Portfile's `go.toolchain_min` doesn't declare and dockhand can't raise. A minimum dockhand raised, to go.mod's `go` directive as go.mod writes it, or one that already gates on its series, is said too, and holds nothing. Only the files of the build systems the port uses hold, as MacPorts says it uses them, by its PortGroups and its configure script: flatbuffers, built with CMake, isn't held by its `package.json` or `Package.swift`, which are said, a manifest's dependencies counted in one line. Where MacPorts can't say, as for a port that builds by its own commands, every file holds. A build file whose change is only the version it names, as `project(nuspell VERSION 5.1.9)`, is said with the line, and holds nothing; any other change to it holds. What it couldn't read, it says, and that holds as a change would: a manifest it can't parse, one that reads another file or declares its dependencies dynamically, and a file past the 1 MiB it reads. A Go module or a Rust crate is compiled into what the port builds, and a check builds with only what the port declares, so what go.mod and Cargo.toml change is counted, "go.mod: 2 added, 4 moved", and holds nothing, nor does what couldn't be read of them. A crate new to Cargo.lock that links a native library, as a `-sys` crate does, is listed with a `·`: it may link a copy it finds installed, and MacPorts may provide the library, for the Portfile to declare. For a Go or Cargo port whose Portfile lists its dependencies, the old and new source archives are compared without them. The current version's archives are compared as MacPorts shipped them, checked against the Portfile's checksums, each with the archive that replaces it in every context that fetches them, as gh's source tarball and older systems' prebuilt zip each are. They're fetched from where MacPorts' own fetch plan finds them, as the new version's are, so a port on a mirror group, such as PyPI's, CPAN's, or GNU's, is compared like any other. Where upstream now serves something else under the same name, as after a stealth update, or nothing, they come from MacPorts' distfiles mirror, under the port's `dist_subdir`; where neither has them, the update says so, and the comparison holds as D4 has it. Patches it couldn't check before the build, a Git-fetched port's, are listed with a `·`; the build applies them. `dockhand diff --archive jq` shows the same comparison file by file. With `--json`, the result's `upstream` holds it: each change, why the archives couldn't be compared if they couldn't, and whether it holds the update for a look before `serve` or `bump` submits it, as a change a build can't catch does, and as archives it couldn't compare do. It is absent for a port with no archives, such as one fetched with Git.

`--revbump-dependents` also bumps the revision of every port that links the updated one directly, found in the port index at the branch's base, so users rebuild them. `--except <port>` leaves one out. `tidy` commits each as "<port>: rebuild for <updated> <version>".

`--submit` goes on to tidy, check, and submit, previewing each step, and submits exactly that commit once its check passes. It takes submit's `--on`, `--tested-binaries`, and `--tested-variants`, and settles where to check before it edits anything. On a terminal the tidy asks for review; `--yes` applies it without asking when it is dockhand's own edit alone, as it does without a terminal. With `--json`, the result is the update's, with `tidy`, `check`, and `submit` holding what those commands report, as far as it went: a script reads the versions, the check's results, and the pull request from one result, and sees where it stopped.

### bump

```sh
dockhand bump jq           # the newest release, all the way to its pull request
dockhand bump jq 1.8.1     # a version you name
```

`bump` is `update --new --submit --yes` asking nothing, for an update you want submitted without looking along the way; `update` is the same work a step at a time. It isn't `port bump`, which refreshes checksums, as `checksums` does. It updates the port on fresh master, in a branch it starts for the edit, tidies the edit into one commit, checks it, and submits exactly that commit once the check passes. Its `--json` result is `update --submit`'s. It takes `update`'s `--revbump-dependents`, `--except`, `--shared-release`, and `--keep-old-checksums`, and `submit`'s `--on`, `--tested-binaries`, and `--tested-variants`.

It stops wherever a person should look:

- Before it edits anything, it stops with nothing changed when an open branch already changes the port, the check has nowhere to build, or the port is already at the release.
- A failed check leaves the branch, with its logs.
- A passing check is held, as `serve`'s are, when the upstream comparison found something a build can't catch or couldn't compare the archives, a commit rule has a finding, or another pull request is open for the port, or couldn't be looked for. `bump` exits 3 and names the `submit` that finishes it after a look; with `--json`, `submit.held` lists why.

The pull request's tested checkboxes stay unticked unless `--tested-binaries` or `--tested-variants` says otherwise: they say what you tested, which dockhand can't. The one exception is a passing `check --variants each` of the port, which ticks the variants item by itself and says which variants it built, since it built them.

### Many ports at once

```sh
dockhand outdated --mine                   # your ports with newer releases
dockhand update --outdated --mine          # one branch each, from fresh master
dockhand update --outdated jq yq fzf       # the ones named
```

`outdated` looks up each port named, or with `--mine` each port whose maintainers line names you (the `maintainer` setting), at master as fetched now. It lists those with newer releases, and counts the rest; `--all` lists every port, and why any couldn't be checked. Interrupted, it prints what it found so far, and `update --outdated` starts nothing. Ports are looked up several at a time, so a thousand take about three minutes. Their requests to GitHub are paced below the 900 a minute GitHub allows, and a thousand ports use about 2,000 of the 5,000 an hour it allows your account, a budget shared with the GitHub CLI and anything else acting for you. `update --outdated` makes one branch per port, each update committed as one commit. It shows how it will split the work before it starts, asks unless `-y`, and with `--check` queues a check of each.

### checksums

```sh
dockhand checksums jq
```

This is what to run after editing a version by hand: it fetches the distfiles the Portfile names, a variant's own included, and writes their checksums. A port whose Portfile lists its crates or Go modules (`cargo.crates`, `go.vendors`) has its own archives refreshed with those lists set aside, as an update computes them. The lists go back byte for byte, since their checksums are Cargo.lock's or go.sum's. Where it can't find a declaration in the Portfile to edit, as one a PortGroup or an `eval` makes, it says which archive's and why, and prints the checksums every archive has now, laid out for the Portfile. A distfile that changed upstream under the same name, in a Portfile the branch hasn't touched, is a stealth update. Dockhand says so and shows the checksums before and after. Since the source changed, it bumps the revision and sets `dist_subdir ${name}/${version}_${revision}`, so mirrors keep both archives. `--no-revbump` leaves the revision, for a change that needs no rebuild, and numbers the directory `${name}/${version}_1` instead. A later version update removes either, where every archive of the new version has a name of its own; where one keeps its name, the line keeps its versions apart on the mirrors, and stays. Both edits are evaluated as MacPorts reads them, and held to changing the port alone: where they would also change another port in the Portfile, such as a subport that inherits the revision, the checksums are refreshed and the rest is left for you, saying why.

### revbump

```sh
dockhand revbump gdal inkscape --subject "rebuild for poppler 25.09.0" --branch poppler-25.09
```

`revbump` increases each port's revision and records the reason as its commit subject, "<port>: <reason>". A revision shared by several subports is bumped for all of them.

### create

```sh
dockhand create https://github.com/owner/project --new --category devel
```

`create` reads a GitHub project, its latest release, and the build files at that release, and writes a new port's Portfile. It uses the github PortGroup, and cargo, golang, cmake, meson, or python as the project's files say. A Rust project's `cargo.crates` come from its `Cargo.lock`, read as `update` reads it, and the checksums are filled in. A crate `cargo.crates` can't fetch, from another registry or from Git, is marked rather than written. What it guessed is marked with a `# dockhand: unconfirmed` comment:
- the license, from the project's `Cargo.toml` or `pyproject.toml` where it declares one MacPorts has a name for, else from GitHub's detection. It's written in MacPorts' words: `MIT OR Apache-2.0` is `{MIT Apache-2}`, and CC0 or the Unlicense is `public-domain`;
- the long description;
- the category, guessed from the build system unless `--category` names it, and said with what it chose, since it picks the directory;
- a Rust or Go port's `destroot`, which installs the programs its manifest names, since neither PortGroup installs anything.

The description is the manifest's one line where it has one, else GitHub's. A plain-HTTP homepage is written as its `https://` form where that answers, since MacPorts prefers HTTPS, and said where it doesn't. The maintainer is your `maintainer` setting, else `nomaintainer`, marked. `--name` names the port when the project's name isn't right for it. The new Portfile is staged, so the next check includes it.

### edit

`edit <port>` opens a port's Portfile in `$VISUAL` or `$EDITOR`, bringing its directory into a sparse worktree first. Without a terminal or an editor, it prints the path. Any other editing is yours to do in the worktree, with any tool; dockhand reads the files as they are.

## Seeing where things are

- **`status`** shows each open branch, with its ports, its work, its latest check, and its pull request each in a column. A check of the files as they are now counts, whichever it was, as submit counts it: a branch restored to files an earlier check passed shows that check. Above them is a list of what needs you, each row ending with the command that moves it forward. Inside a worktree, or naming a branch, it shows that branch in detail. `--attention` prints only what needs you and exits 3 when anything does, for a prompt or a script. `--port <port>` finds every branch touching a port, `--all` includes merged, closed, and archived branches, and `--refresh` reads your pull requests from GitHub first. Run in a ports checkout with no command, dockhand shows status.
- **`watch`** is status kept current as `serve` works. On a terminal, a line such as `c <branch>` checks a branch, `l` shows its latest logs, `t` tidies it, and `s` submits it, each with its usual confirmations. `--plain` prints events as lines instead, for scrollback, SSH, and screen readers.
- **`diff`** shows what the branch changes from its base, edits included, which is what a check captures and a pull request shows. It lists first the ports CI would build, each marked changed or revision-only. `--stat` lists the files, and `--archive [<port>...]` compares the source archives instead.
- **`impact`** lists the ports the branch changes, the ports that depend on them directly, and the shared files it changes with the ports that load them. They are candidates to look at, not proof of anything: `check --also` builds some of them against the branch.

## Checking

```sh
dockhand check                   # the working files, where check.on says
dockhand check --on tahoe        # on this release's Tart image
dockhand check --plan            # what would be built, building nothing
```

A check builds every port the branch changes by MacPorts CI's rule. A change to a Portfile or under `files/` marks its directory, and every subport of a marked directory is a target, less those replaced, known to fail, or unsupported on the platform. Each environment evaluates the ports for itself, and builds its targets in its own dependency order: a port may need a library on one release and not another, or exist on one architecture only. The plan shows one `Order` where the environments agree, and each one's where they don't. In each environment, every target is linted, then fetched, checksummed, installed, and tested in turn, with exactly its dependencies active. Declared tests are advisory, as they are in MacPorts CI. `--tests required` makes them count, and `--tests skip` leaves them out; `check.tests` sets the default. The policy is applied the same way whichever provider built the port: under `--tests required`, tests that failed or timed out fail it at the test phase. A port that declares no tests, by MacPorts' reading of its `test.run`, passes under any policy; a plan requiring tests names it, "No tests    ov declares none", and its result reads "✓ declares no tests". GitHub's workflow runs its own tests whatever the policy, so under `--tests skip` they run there, and don't count; the plan says so. A result keeps the policy of the check that built it. When a later check of some ports asks for a different policy, an earlier result reads under its own, such as "tests failed (advisory, check-3)", and doesn't hold up a submission.

Results are per target and per environment:

| Result | Meaning |
| --- | --- |
| passed | linted, fetched, checksummed, and installed; its tests count only when required |
| failed | one of those failed; the log says which, and a failure's summary adds the first compiler error it finds there, marked "from its log" |
| blocked | a changed port it needs failed, so it wasn't built |
| unmet | the environment can't build it, such as a port needing Xcode where there is none |

**What it captures.** By default, the tracked files as they are on disk, committed or not, as a numbered snapshot. `--staged` checks the index, and `--head` the committed tip. `--working-tree` checks the working files of a `--branch` checked out elsewhere, and `--include <file>` adds an untracked file without staging it.

**What it builds.** `--only <port>` narrows the check to some of the changed ports, and adds back the changed ports they need. `--also <port>` builds unchanged ports against the branch, such as the dependents `impact` lists.

`--variants` builds one port its way: the port `--only` names, or the one port the branch changes. There are two forms:
- `--variants +tests` builds it with those variants in place of its defaults, as MacPorts' command line writes them (`+tests -docs`).
- `--variants each` builds it with its defaults, then once with each variant it declares, over its defaults. The variants among its defaults are left out, since its default build has them, and so is `universal`, which needs other architectures' dependencies a clean VM doesn't have.

Each build is evaluated with its variants, so it's planned as MacPorts would build it: whether it needs Xcode, whether it declares tests, and what it depends on can change with a variant. A build is left out of a release whose port doesn't declare its variant there. A variant the port doesn't declare anywhere is refused before anything builds. Each build is its own row, `s2n-tls +tests`, in the plan, the results, and the pull request.

`each` makes one whole build per variant per environment. Above 12 builds, `check` asks first; without a terminal, `--yes` builds them. GitHub's workflow builds default variants only, so `--variants` there is refused.

**Where it builds.** `--on` names where, and every one named must pass; repeat it for several. Without it, `check.on` decides, and without that, your command provider if you have one, else Tart on this Mac's release. The forms are:

| `--on` | Builds on |
| --- | --- |
| `tart` | this Mac's release |
| `tart:tahoe`, `tart:sequoia,tahoe`, `tart:all` | those releases' images, or every image you have |
| `tahoe`, `15` | shorthand for `tart:` that release |
| `github` | MacPorts' workflow in your fork |
| `command` | your own script |

With several releases, `check` builds two at a time, as many as macOS runs VMs, and shows the results as a grid, one column per release. A check on several providers, such as `--on tahoe --on github`, builds on each at once.

**Reusing what didn't change.** Each result keeps what its build read: the environment, the port's directory and `_resources`, and every port that was active. When every port an environment would build reads what an earlier passed build of it read, and in the environment as it is now, the check reuses those results and builds nothing there. It says so ("reuses that result, building nothing"), and `logs` names the run that built each port. A rebase that leaves a port's files and dependencies alone keeps its result, and one that changes `_resources` doesn't. `--fresh` builds everything.

**Running it.** With no `serve` running, the check runs in the foreground and says so. Ctrl-C stops it, keeping what finished. With `serve` running, the check is handed to serve and followed here, and Ctrl-C only stops following. `-d` queues it and returns.

A check whose process dies without settling it, killed or lost with its terminal, is shown as **stopped** by `status` and `queue`. `dockhand wait` resumes it where it stopped, and so does the next `serve`; `dockhand cancel` ends it. `clean` removes the clone it left, once nothing runs it.

One check of a branch runs at a time. While one is queued or running, `check` refuses, and `--replace` stops it, keeping what it finished, and checks the files as they are now. `submit --check`, `update --submit`, and `retry` refuse too, naming the check to wait for or cancel; a baseline looks beside the check it explains, and a check asked to stop no longer counts. A misspelled `--tests` is refused before anything is captured.

### After a check

- **`logs check-12`** lists the check's provider runs and where each port's log is; `--port jq` prints one. `logs tart_7y62p4sigena6xlr` looks up one provider run by the ID a pull request names, or by the provider's own reference, such as a workflow run's URL. In a branch's worktree, `logs` alone is the branch's latest check.
- **`retry check-12`** queues the same check again: the same files, plan, and environments, whatever the branch holds now.
- **`check --baseline`** builds the ports that failed at install or test in the branch's latest check, or whose tests failed where the policy only reports them, or the `--only` ones, at the master that check started from, and reports each beside the branch's result, its tests' included: "its tests fail at the base too, as in check-38, so they did before this branch". It shows whether master fails the same way, and nothing more. `--plan` shows the baseline it would run, and runs and records nothing.
  - It is planned as a check would be, from master's own Portfiles in each of the check's environments, so a port master builds only with Xcode is unmet where there is none. It builds the ports it names, not the rest of their subports.
  - It rebuilds each port only in the environments where it failed, or its tests did, and reports it there. A port named with `--only` that failed nowhere is rebuilt everywhere the check built it.
  - A port that failed only before building, at lint, fetch, or checksum, is left out unless `--only` names it: those failures come from the branch's own Portfile and distfiles. So is a port the branch adds, since master has nothing to compare.
  - A failed check points to it when a baseline can answer something, naming the ports and the master: `To see whether jq fails at master 1a2b3c4 too: dockhand check --baseline --branch jq-update`.
  - Baselines are off by default. With `check.baseline = true`, a failed check runs the one it points to by itself.
- **`queue`** lists the checks queued and running, **`wait check-12`** follows one until it ends, and **`cancel check-12`** stops one, keeping what finished. In a branch's worktree, `wait` and `cancel` with no check named take the branch's latest, as `logs` does.

## Providers

### tart

Each check clones one of dockhand's images for each release and attempt, and deletes the clone afterwards ([details](tart-provider.md)). The images live in `~/.dockhand/tart`, or `$DOCKHAND_TART_HOME`, apart from your own Tart VMs.

```sh
dockhand providers setup tart               # this Mac's release
dockhand providers setup tart sequoia       # another; names like tahoe or numbers like 15
dockhand providers setup tart --xcode ~/Downloads/Xcode_26.xip   # the Xcode add-on for this Mac's release
dockhand providers setup tart --check       # check the image in a disposable clone
dockhand providers setup tart --rebuild     # a replacement, keeping the old one until the new one passes
```

An image starts from Cirrus Labs' vanilla macOS image, and holds the Command Line Tools of the release's pinned generation and MacPorts: the release dockhand pins, unless `--macports-version` names another. Making one downloads the vanilla image the first time and takes up to 60 GB of disk. A golden copy is kept beside it, and a lost image is restored from it. Golden Gate, macOS 27, needs Tart 2.39.0 or newer, since its images have ASIF disks, which older Tart can't list while they run; setup says so before it starts anything.

Setup records what each image was made from: the vanilla image by digest, and the tools and MacPorts it installed. A check's results stand for the image it ran in. Once an image is made again from a newer vanilla image, or with other tools, what passed in the old one no longer counts, and `status` names the ports to check again. Rebuilding from the same source with the same tools keeps them.

Xcode is an add-on. `--xcode`, given an Xcode `.xip` from Apple or a folder of them, makes `dockhand-xcode-<release>`: the same image with Xcode too, in up to 65 GB more. Its Xcode is the one MacPorts' arm64 buildbot for the release runs, such as 26.6 on Tahoe, so ports build as MacPorts builds its packages. `providers.tart.xcode` names another for a release. Setup needs the archive of exactly that version, and says which to download when the folder lacks it; betas are never chosen. With `xcodes` installed (`sudo port install xcodes`), setup downloads it for you, signing in with your Apple ID through xcodes: at a terminal it asks first, and without one it uses the sign-in xcodes kept from the last time. When a release has an Xcode image, checks on it use it. A port that needs Xcode, itself or through a changed prerequisite, is built only there; without it, the port is unmet, not failed, and the check says to add the image.

macOS runs at most two VMs at once, your own among them, so `serve` runs one Tart check at a time unless `providers.tart.capacity` says otherwise. `providers.tart.test_timeout` bounds each target's tests, 30 minutes by default.

### github

`--on github` builds with MacPorts' own CI workflow in your fork's GitHub Actions, the way a pull request is built ([details](github-provider.md)). It needs the GitHub login, a remote that pushes to your fork, and Actions enabled on the fork, which GitHub turns off for new forks.

### command

Your own script, for a build box or a VM you manage. Dockhand gives it a request file and reads back a result file. It can't vouch for how the script built, so results read "reported by <name>" ([details](command-provider.md)).

```toml
[providers.command]
run = "~/bin/build-ports"
name = "buildbox"
```

## Shaping the commits

```sh
dockhand tidy
```

`tidy` proposes the commits a reviewer should see: by default one per port directory, with the subject dockhand's commands recorded or the one your own commits give, and every uncommitted edit included. The files come out exactly as you have them; tidy never changes a file.

On a terminal, tidy shows the proposal and lets you review the diff, change the groups, edit the messages, and apply it. `-y` applies a plan made only of dockhand's own edits without asking, and that is also the only plan tidy applies without a terminal; anything else needs review. `--squash --message "port: what changed"` makes one commit of the whole branch, applied as given, since the message is yours. `--group "2 1+3"` rearranges the proposal: commit 2 first, then one commit of 1 and 3. A commit that would come before one it depends on, as the latest check of the files orders their ports, carries a note saying so; the order is still yours. `--author "Name <email>"` attributes a commit that combines several people's. `--plan --out plan.toml` saves the plan as TOML, each message as the commit will say it, to edit as plain text; `--apply plan.toml` shows each commit's whole message as it will be written, and applies it if the branch hasn't changed since.

Before rewriting, tidy keeps the old history as a checkpoint, such as `tidy-3`. `dockhand restore tidy-3` puts it back when nothing has been committed since; the files aren't touched, so edits tidy committed read as uncommitted again. `rebase` keeps one the same way, such as `rebase-4`. Restoring it puts back the files as they were before the rebase, and the master the branch started from, so master's newer files don't read as the branch's edits; a change to one of those files stops it. A rebase checkpoint made before dockhand kept the base puts back the history and files, and says the branch still counts from the newer master until the next `rebase`.

One `tidy`, `rebase`, or `restore` of a branch runs at a time; another waits. If one is stopped part-way, killed or lost with its terminal, the next of them on that branch finishes it from what Git shows: a change that was made is recorded, and one that wasn't is dropped, so `restore` of it says it was never made.

Tidy writes commits by MacPorts' rules, and `submit` checks them: a subject naming the port, short and specific; a body wrapped at 72; tickets as full URLs; no merge or follow-up commits; the revision reset when the version changes. Each finding ends with a code in brackets, and `dockhand explain <code>` says what the rule asks and where MacPorts asks it.

## Submitting

```sh
dockhand submit --plan     # the preview: commits, destination, title, checks, upstream, rule findings, and the description
dockhand submit
```

`submit` pushes the branch's committed head to your fork and opens its pull request against `macports/macports-ports`, or updates the one it has. The preview also lists any other open pull requests for the same ports, and what comparing each update's upstream archives found, as the update reported it: a `!` line is what a reviewer would ask about, and holds a submission nobody reviews, bump's or serve's, as a legend beside it says; a `·` line holds nothing. `submit --passing` lists them under the same label. They're yours to weigh; a submission of yours isn't held for them, as `serve`'s and `bump`'s are. With `--json`, `upstream` holds them, by port. A commit whose `Generated-By` names a dockhand built from uncommitted source is flagged too, since nobody else can find that build; `modified_builds` lists them.

The committed files must have passed a check, for every changed port in every environment. Because a check builds the files rather than the commits, a check before `tidy` covers the commits `tidy` makes.

- `--accept <port>` acknowledges a failed extra from `--also` or a failed revision-only port. The pull request says the cause wasn't established. An extra no check built asks nothing of submit, status, or serve, and there is nothing of it to accept: it's built for what it shows.
- `--draft` opens a draft, which unfinished or failing checks allow, and `--ready` takes it out of draft once its commit passes; `--ready --plan` says what it would do, and a draft's preview names it. GitHub can refuse dockhand that, as an organization restricting which apps may act for its members does; the macports organization does. dockhand then runs `gh pr ready` itself, when the GitHub CLI is installed and signed in as the account dockhand is, since that CLI signs in as its own app, and says it did. Otherwise the error names the pull request's page and `gh pr ready`, and why the CLI wasn't used. Only the organization's owners can let dockhand's own app through: on GitHub, Settings → Applications → Authorized OAuth Apps → dockhand, then Request access beside the organization, which asks them.
- `--no-check` submits without a check, and the pull request says no local build ran.
- `--check` checks the committed head first and submits exactly that commit once it passes; running it is the decision. `--on` says where.
- `--passing` goes through every branch whose check passed for exactly what it would submit, asking about each. It needs a terminal, and takes no `--yes`: a script submits one branch at a time, with `submit --branch <name> --yes`.
- `--head` submits the committed head, leaving uncommitted edits out.

Every push is conditional on your fork's branch being where submit last saw it, so nobody else's push is ever overwritten. A description you edited on GitHub is kept: submitting again refreshes only what dockhand wrote, while it's still as dockhand wrote it. That is the Description, the commit's body or a table of the commits, so a body written since the pull request opened reaches it; the Type(s), which only gain ticks that way; and everything from Tested on down. `--type` replaces the Type(s) however they read, or adds them before Tested on to a description that has none. A submit that is interrupted is finished by running it again, and never opens a second pull request: when opening one fails, submit looks for one on the branch at once, since GitHub may have opened it before the reply was lost, and the next submit finds one it opened but didn't record. The title is the commit subject unless `--title` says otherwise, and an existing pull request is never retitled unless you pass `--title`.

The description follows MacPorts' pull request template, beginning "Submitted by **dockhand**", dockhand's name in bold and linked, and ending with a line naming dockhand's version. A description dockhand wrote before its first line did so gains it when submitted again, where the last line is rewritten. `--type` fills in its Type (bugfix, enhancement, security fix); without it, a branch whose commits are all dockhand's own and include an update it made is an enhancement, and a commit citing a CVE adds security fix. `--tested-variants` and `--tested-binaries` tick its checkboxes, and `--skip-notification` keeps maintainers from being mentioned. *Tested on* says what each environment was, from what the guest reported: the macOS version and build, the architecture, and the Xcode or Command Line Tools version. Each line ends with how it was built and the ID of each provider run, such as `tart: built in a clean VM (Run ID: tart_7y62p4sigena6xlr - checked in check-12)`. The last line names the dockhand that submitted it. Anything that wasn't recorded is left out rather than guessed.

### After review

`status --refresh`, or `serve` every few minutes, reads your pull requests' state, reviews, and CI. To answer a review, edit the branch, check it, tidy, and submit again; the pull request is updated. When reviewers asked for changes, submit then asks whether to request their review again; `submit.rerequest_review` can make that `always` or `never`. `rebase` replays the branch onto fresh master when it needs that, keeping a checkpoint. It makes the replayed commits before moving anything, so a rebase that conflicts leaves the branch as it was, and names the files. A branch with a merge commit is rebased by hand.

When a pull request is merged, its branch is marked merged. `dockhand review <pr>` applies the same commit rules to anyone's pull request. It posts nothing unless you say so: `--comment`, `--request-changes`, or `--markdown` to print the text for pasting.

## serve

```sh
dockhand serve               # in a terminal of its own
dockhand serve --install     # as a launchd agent that starts at login
dockhand serve --drain       # run what is queued now, then exit
```

`serve` runs queued checks, people's before its own, and keeps running new ones as they are queued. One serve leads. A second stands by and takes over if the leader dies. Stopping serve leaves the check it was running for the next serve, which picks it up where it stopped.

Between checks it:

- reads your open pull requests every few minutes, so `status` shows their reviews and CI, and marks a merged one's branch merged;
- once a day, at `serve.outdated_at`, looks for new releases of your ports and does what `serve.for_outdated` says: `list` counts them for status, `draft` prepares a branch for each, and `check` also checks each;
- once a day, unless `cleanup.automatic = false`, cleans up automatically, as below;
- posts macOS notifications as checks finish and pull requests change. They are posted through AppleScript, so macOS credits them to Script Editor, and clicking one opens it. `serve.notify = false` turns them off, and `--no-notify` turns them off for one run.

Serve opens no pull requests by default. With `--submit-passing`, or `serve.submit_passing = true`, it opens one for each branch it prepared whose check passed, at most `serve.submit_limit` a day. It never opens one with an upstream or commit-rule finding, one whose archives couldn't be compared, one needing `--accept`, or one for a port another open pull request updates; those wait on the attention list, and the pull request says serve opened it without a person's review. `--no-submit-passing` turns it off for one run.

`--install` runs the agent with the flags given beside it, such as `--no-notify`, and with the settings serve was installed under: `--git`, `--db`, and the environment variables below that serve reads, each path made absolute. Run it again after changing them. `--uninstall` removes it.

## Cleaning up

- **`clean`**, which is `clean --merged`, removes a merged branch's worktree, local branch, and fork branch, each only while it still holds the merged commit. A worktree with edits or untracked files, and work that went on past the merge, are kept. So is a branch checked out anywhere clean doesn't remove, your checkout or a worktree dockhand didn't make, since deleting it would leave that checkout on a branch that's gone.
- **`clean --closed`** and **`clean --archived`** take only the worktrees of branches whose pull request closed unmerged, or that you archived. Their work isn't merged, so the branches and checkpoints stay, and `path` or any command that needs the worktree checks it out again.
- **What checks left behind.** A check deletes its Tart clone when it ends, and a later attempt of the same check deletes an earlier one's. A check whose process dies with no later attempt leaves its clone behind, sometimes still running and holding one of the Mac's two VM slots. Whichever branches it cleans, `clean` lists these clones too, and removes one once no process is running its check, stopping it first. It keeps a clone no check of this checkout made, since another database may be using it, and it never touches the images checks clone from.
- **`archive [branch]`** hides a branch from status without touching anything; `status --all` still shows it, and `archive --undo` brings it back.

`clean` shows what it would remove first. On a terminal it asks, and a script passes `--yes`. A branch's record always stays, so `status --all` still lists it, as cleaned, and `status <branch>` still finds it. Check logs in `~/.dockhand/logs` are kept.

**Automatic cleanup.** Once a day, dockhand cleans up by itself, unless `cleanup.automatic = false`. It removes:
- what `clean --merged` would;
- what checks whose process died left behind;
- port indexes unused for `cleanup.after`;
- the journal's events older than `cleanup.after`, and the sessions that ended or went quiet before then, but for one a lease still names;
- the archives Tart's builds made, which checks keep in `~/.dockhand/archives` for later builds to install, once no open branch's check names them and none has named them within `cleanup.after`. It says how much stays;
- the vanilla images Tart pulled for `providers setup tart`, once unused for 30 days. Each is deleted from dockhand's own Tart home with `tart delete`, never `tart prune`, and the next setup of its release downloads it again.

`serve` runs it. Without serve, a command starts it in the background once its own work is done, and doesn't wait for it; what it removed goes to `cleanup.log` beside the database. When free space where the database or Tart's images are falls below `cleanup.min_free`, it runs at once, and says so, at most once an hour.

## Scripting

- **Progress** goes to standard error as the work goes: what you need to follow it, such as a port index being built, which can take minutes. `-v` adds the work behind the scenes, and `-vv` every step. A check dockhand runs itself reports its own steps; `-v` shows what the engine and its providers say beneath them.
- **`--json`** writes one envelope on standard output when the command ends: `{"version": 1, "command": "...", "exit_code": 0, "error": null, "result": {...}}`. A command's result has one shape wherever it stops, and its exit code says where.
  - What it didn't reach is left out or empty.
  - The steps it goes on to are inside it:
    - `update --submit`'s and `bump`'s result is the update's, with `tidy`, `check`, and `submit` inside;
    - `submit --check`'s is the submission's, with `check`;
    - `check`'s is the check's, with the `baseline` that `check.baseline` runs, whose `run.baseline_of` names the check it looks into.
  - A command that refuses before doing anything reports only its error.
- **Exit codes:** 0 for success, 1 for an error, 2 for a failed check, 3 when something needs attention (`status --attention`, or a `bump` held for a look), and 130 for an interrupt.
- **Without a terminal**, nothing is asked. A command that would ask refuses and says what it needs, or proceeds where `-y` is given. `tidy` applies only a plan made of dockhand's own edits, or one you give it.

## Settings

`dockhand config` lists each of these with its value and whether it came from the file.

| Key | Default | What it does |
| --- | --- | --- |
| `worktrees` | `~/Source/macports-branches` | where branch worktrees go |
| `maintainer` | none | your maintainers line, for `--mine`, serve's daily look, and `create` |
| `check.on` | command if set up, else Tart on this Mac's release | where checks build |
| `check.tests` | `declared` | `declared` (advisory), `required`, or `skip` |
| `check.baseline` | `false` | run a baseline after a failed check |
| `submit.rerequest_review` | `ask` | after pushing to a pull request with changes requested: `ask`, `always`, or `never` |
| `cleanup.automatic` | `true` | the daily automatic cleanup |
| `cleanup.after` | `7d` | how long a port index goes unused, or a kept archive unnamed by an open branch, before cleanup removes it |
| `cleanup.min_free` | `30GB` | the free space below which cleanup runs at once |
| `serve.for_outdated` | `list` | `list`, `draft`, or `check` |
| `serve.outdated_at` | `07:00` | when serve looks for new releases, in local time |
| `serve.submit_passing` | `false` | open pull requests for serve's passing updates |
| `serve.submit_limit` | `10` | the most pull requests serve opens a day |
| `serve.notify` | `true` | macOS notifications |
| `providers.tart.capacity` | `1` | Tart checks serve runs at once |
| `providers.tart.test_timeout` | `30m` | the bound on one target's tests |
| `providers.tart.xcode.<release>` | what MacPorts' arm64 buildbot runs | the Xcode a release's Xcode image installs, by release name or number |
| `providers.github.remote` | the one remote pushing to your fork | which remote, when several push to forks you own |
| `providers.github.capacity` | `2` | github checks serve runs at once |
| `providers.command.run` | none | your build command, given the request file's path |
| `providers.command.name` | `command` | how its results are labelled |
| `providers.command.capacity` | `1` | command checks serve runs at once |

### Environment

| Variable | What it does |
| --- | --- |
| `MACPORTS_TREE` | the ports checkout, when `--tree` isn't given |
| `DOCKHAND_DB` | the database, when `--db` isn't given; `~/.dockhand/dockhand.db` otherwise. A newer dockhand migrates it forward, and an older one can't open it after: the migration says so, and keeps the database as it was beside it, `dockhand.db.schema-24` for one migrated from schema 24, for 30 days after a newer copy is made |
| `DOCKHAND_CONFIG` | the configuration file |
| `GIT_BIN` | the Git executable, when `--git` isn't given |
| `GH_TOKEN`, `GITHUB_TOKEN` | a GitHub token, ahead of any saved login |
| `DOCKHAND_TART_HOME` | where dockhand's Tart images are; `~/.dockhand/tart` otherwise |
| `TART_HOME` | your own Tart home, read to count your running VMs against the Mac's two; `~/.tart` otherwise |
| `DOCKHAND_SSH_DIR` | the keys dockhand reaches its Tart guests with; `~/.dockhand/ssh` otherwise |
| `DOCKHAND_UPSTREAM` | where master is fetched from, a mirror or a local repository, rather than MacPorts' own |
| `DOCKHAND_INDEX_MIRROR` | the directory port indexes are downloaded from, a nearer mirror's, rather than MacPorts' |
| `DOCKHAND_INDEX_CACHE` | where port indexes are cached; `dockhand/indexes` in your cache directory otherwise |
| `DOCKHAND_GITHUB_CLIENT_ID` | the OAuth application `auth login` uses |

`serve --install` keeps the ones serve reads, as they are when it's run, in the agent: `DOCKHAND_CONFIG`, `DOCKHAND_UPSTREAM`, `DOCKHAND_INDEX_MIRROR`, `DOCKHAND_INDEX_CACHE`, `DOCKHAND_TART_HOME`, `TART_HOME`, and `DOCKHAND_SSH_DIR`, with `--git` and `--db`. A token is never kept there, since anyone on the Mac can read the agent's file.
