# Checks

```sh
dockhand check                   # the working files, where build.default's profile says
dockhand check --on tahoe        # on this release's Tart image
dockhand check --plan            # what would be built, building nothing
```

A check builds every port the branch changes by MacPorts CI's rule. A change to a Portfile or under `files/` marks its directory, and every subport of a marked directory is a target, less those replaced, known to fail, or unsupported on the platform. Each environment evaluates the ports for itself, and builds its targets in its own dependency order: a port may need a library on one release and not another, or exist on one architecture only. The plan shows one `Order` where the environments agree, and each one's where they don't. In each environment, every target is linted, then fetched, checksummed, installed, and tested in turn, with exactly its dependencies active. Declared tests are advisory, as they are in MacPorts CI. `--tests required` makes them count, and `--tests skip` leaves them out; `check.tests` sets the default. The policy is applied the same way whichever provider built the port: under `--tests required`, tests that failed or timed out fail it at the test phase. A port that declares no tests, by MacPorts' reading of its `test.run`, passes under any policy; a plan requiring tests names it, "No tests    ov declares none", and its result reads "✓ declares no tests". GitHub's workflow runs its own tests whatever the policy, so under `--tests skip` they run there, and don't count; the plan says so. A result keeps the policy of the check that built it. When a later check of some ports asks for a different policy, an earlier result reads under its own, such as "tests failed (advisory, check-3)", and doesn't hold up a submission.

Results are per target and per environment:

| Result | Meaning |
| --- | --- |
| passed | linted, fetched, checksummed, and installed; its tests count only when required |
| failed | one of those failed; the log says which, and a failure's summary adds the first compiler error it finds there, marked "from its log" |
| blocked | a changed port it needs failed, so it wasn't built; an earlier check's block fills in for a later check only while the failure that blocked it does |
| unmet | the environment can't build it, such as a port needing Xcode where there is none, or a newer Xcode than it has, as its `minimum_xcodeversions` asks of the release, "needs Xcode 26.0"; the remedy says how to give the image that Xcode |

**What it captures.** By default, the tracked files as they are on disk, committed or not, as a numbered snapshot. `--staged` checks the index, and `--head` the committed tip. `--working-tree` checks the working files of a `--branch` checked out elsewhere, which asks for one or the other where it has commits and edits too; one with no commits has only its working files to check, so it takes them. `--include <file>` adds an untracked file without staging it.

**What it builds.** `--only <port>` narrows the check to some of the changed ports, and adds back the changed ports they need. `--also <port>` builds unchanged ports against the branch, such as the dependents `impact` lists.

**A port fetched with Git.** Its `git.branch` names a tag or a branch, which binds nothing, so the plan resolves it to the commit it names as the check is planned, and says so: "libharbor: git.branch v4 names 1a2b3c4 now, which its build must fetch". A build that fetched another commit fails at fetch, and a result stands for a later check only for the commit that check expects. A port `--only` left out is shown too, marked "(left out)", since an earlier check's result of it stands only where it fetched the commit its tag names now. With `--json`, each of the plan's builds holds what it builds under `git`, and what `--only` left out under `omitted_git`.

**A port fetched as a GitHub tag's archive.** The tag binds nothing either: a project can move it, a stealth update, and the archive fetched then isn't the one the Portfile's checksums describe. The plan records the commit each such tag names, read with `git ls-remote`, which spends none of GitHub's API budget; a version set up from a commit has nothing to move, and records none. A later check doesn't reuse a build whose tag has moved since, and says so: "libharbor builds again: v4 now names d4c0fdb, not 1d15bc8, as when check-41 built it; dockhand checksums libharbor handles a stealth update". A submission resting on that check is held for the same reason. With `--json`, each of the plan's builds holds them under `tags`.

`--variants` builds one port its way: the port `--only` names, or the one port the branch changes. There are two forms:
- `--variants +tests` builds it with those variants in place of its defaults, as MacPorts' command line writes them (`+tests -docs`).
- `--variants each` builds it with its defaults, then once with each variant it declares, over its defaults. The variants among its defaults are left out, since its default build has them, and so is `universal`, which needs other architectures' dependencies a clean VM doesn't have.

Each build is evaluated with its variants, so it's planned as MacPorts would build it: whether it needs Xcode, whether it declares tests, and what it depends on can change with a variant. A build is left out of a release whose port doesn't declare its variant there. A variant the port doesn't declare anywhere is refused before anything builds. Each build is its own row, `s2n-tls +tests`, in the plan, the results, and the pull request.

`each` makes one whole build per variant per environment. Above 12 builds, `check` asks first; without a terminal, `--yes` builds them. GitHub's workflow builds default variants only, so `--variants` there is refused.

**Where it builds.** A build profile says where: `--build` names one, and `build.default` is the profile otherwise, `host` unless set, or `command` where your own script is set up. Checks submitted without interactive review, such as those run by `serve` or `bump`, use `build.unattended`. Its default is `minmax` when Tart is installed, or `build.default` otherwise. dockhand's own profiles all run on Tart:

| `--build` | Builds on |
| --- | --- |
| `host`, `local` | this Mac's release |
| `minmax` | each port's oldest and newest release you have an image of, macOS 12 at the earliest; a check of several ports covers the union of theirs, and each port builds on its own two alone |
| `macports-ci` | the releases MacPorts' CI builds every pull request on, as its workflow at master names them (macOS 14, 15, and 26 today) |
| `all` | every release you have an image of |

A profile of your own is a provider and its releases, a list or one of the rules above: `[build.profiles]` `ci-exact = { provider = "github", releases = "macports-ci" }`, or `mine = { releases = ["15", "26"] }`. A port its Portfile's platforms exclude on a release is left out there, as always.

`--on` names explicit environments instead, and every one named must pass; repeat it for several. The forms are:

| `--on` | Builds on |
| --- | --- |
| `tart` | this Mac's release |
| `tart:tahoe`, `tart:sequoia,tahoe`, `tart:all` | those releases' images, or every image you have |
| `tahoe`, `15` | shorthand for `tart:` that release |
| `github` | MacPorts' workflow in your fork |
| `command` | your own script |
| `local`, `local:/opt/macports-test` | a MacPorts prefix on this Mac, `/opt/local` unless named ([the local provider](../local-provider.md)) |

A check that built on fewer releases than MacPorts' CI does is said in `submit`'s preview and under the pull request's Tested on table: "MacPorts CI also builds on macOS 14 and 15, which no check here built on." A check on `github` runs that workflow itself, so it covers them all. `--build macports-ci` checks on each; it isn't the default, since it builds three times over.

With several releases, `check` builds two at a time, as many as macOS runs VMs, and shows the results as a grid, one column per release. A check on several providers, such as `--on tahoe --on github`, builds on each at once.

**Reusing what didn't change.** Each result keeps what its build read: the environment, the port's directory and `_resources`, and every port that was active. When every port an environment would build reads what an earlier passed build of it read, and in the environment as it is now, the check reuses those results and builds nothing there. It says so ("reuses that result, building nothing"), and `logs` names the run that built each port. A rebase that leaves a port's files and dependencies alone keeps its result, and one that changes `_resources` doesn't. `--fresh` builds everything.

**Keeping a guest to look in.** `check --keep` keeps each Tart environment's clone, stopped, where its guest finished, whether its ports passed or failed, but not after trouble the guest didn't finish through. A kept clone holds disk, not a VM slot. Automatic cleanup spares it for a day unless space is low, and `clean` or the branch's next check removes it. `dockhand shell check-12 -- otool -L /opt/local/bin/harbor` starts it, runs the command in the port's work directory, prints what it says, exits with its code, and stops the clone again; with no command, on a terminal, it opens a shell there. A check that kept clones on several releases needs `--on <release>`, and `--port` picks the port of several. `dockhand shell check-12 --test` copies the port's directory as it is in the branch now into the clone and runs its test phase alone again, with `port -o`, on the build as it stands: an edit outside the test phase doesn't apply, and the run is never a check's result.

**Running it.** With no `serve` running, the check runs in the foreground and says so. Ctrl-C stops it, keeping what finished. With `serve` running, the check is handed to serve and followed here, and Ctrl-C only stops following. `-d` (`--enqueue`) queues it and returns: with no `serve` leading, it starts `serve --drain` in a session of its own, notifying nobody and logging to `drain.log` beside the database, and says so, so the check runs after the command, or the tool that ran it, has ended; `dockhand wait` follows it. A drain that finds the queue empty reads it once more after letting go, so a check queued meanwhile isn't left.

If a check's process exits unexpectedly, for example because it was killed or its terminal closed, `status` marks it **stopped** in both the branch row and the check list. `dockhand wait` resumes it where it stopped, and so does the next `serve`; `dockhand cancel` ends it. `clean` removes the clone it left, once nothing runs it.

One check of a branch runs at a time. While one is queued or running, `check` refuses, and `--replace` stops it, keeping what it finished, and checks the files as they are now. `ship` and `retry` refuse too, naming the check to wait for or cancel; a baseline looks beside the check it explains, and a check asked to stop no longer counts. A misspelled `--tests` is refused before anything is captured.

## After a check

- **`logs check-12`** lists the check's provider runs and where each port's log is; `logs check-12 jq` prints one. The run commands, `logs`, `wait`, `cancel`, and `retry`, take `-b`, `-p`, and `--pr` for a branch's latest check, from anywhere: `logs -p jq` prints jq's log from the latest check of its branch, and `logs -b jq-update` lists that check's runs. A port's log on Tart holds its dependencies' installs before its own build, which can be most of it: hugo's own began near line 46,400 of 47,000. So where the log's steps were recorded, it lists each, `lint`, `dependencies`, `fetch`, and the rest, with the line it begins on, and prints from the port's own build on, saying how many lines it left out; `--full` prints the whole log, as it's printed for results recorded before steps were, and from other providers. `--phase <step>` prints one step's lines alone, `logs check-12 jq --phase fetch`, from the line it began on to where the next step began, which with `--json` gives the `phase` and the line it's `from`; a build that recorded no steps, or not that one, says which it did. While the port builds, `logs check-12 jq --follow` prints its log as it grows, in any process, from the copy the check keeps of it at each read of the guest, and the rest once its build ends; a check on GitHub Actions, whose log comes back with its run, says it can't be followed and names the run's page. Where the check built the port in more than one place, it opens the build that failed, or whose tests did, before one that passed, and names the others with the `logs <run> <port>` that opens each; `--json` gives the build's `execution` and the others under `elsewhere`. With `--json`, the whole log comes with its `steps` and the line its `own_build` begins on. `logs tart_7y62p4sigena6xlr` looks up one provider run by the ID a pull request names, or by the provider's own reference, such as a workflow run's URL. In a branch's worktree, `logs` alone is the branch's latest check.
- **`artifacts [branch]`** prints the archive each port of the branch's latest check built, a line for each port and platform it passed on: the port, the platform, and the archive's path on this Mac, for `otool` or a smoke test by hand. A port that didn't pass built none, and one whose archive wasn't kept, or cleanup removed, says so; `--json` gives each `port`, `platform`, `name`, and `path`, or why it's `missing`. In a branch's worktree, `artifacts` alone is that branch's; `-b`, `-p`, and `--pr` name one from anywhere.
- **`retry check-12`**, or `retry` in a branch's worktree for its latest, queues the same check again: the same files, plan, and environments, whatever the branch holds now. `retry --ci -b <branch>` instead runs the branch's pull request's failed MacPorts CI jobs again, through your GitHub login, which needs write access to MacPorts' repository, as a maintainer has. Status names it where every failed job failed fetching an archive, as a mirror refusing one: `status --refresh` reads each failed job's log once, and says "#35174 MacPorts CI failed fetching jni-sys-macros-0.4.1.crate (mirror.sjtu.edu.cn answered 403) on macos-15". A failure no reading names is said as before, with the checks' page to look at.
- **`check --baseline`** builds the ports that failed at install or test in the branch's latest check, or whose tests failed where the policy only reports them, or the `--only` ones, at the master that check started from, and reports each beside the branch's result, its tests' included: "its tests fail at the base too, as in check-38, so they did before this branch". It shows whether master fails the same way, and nothing more. `--plan` shows the baseline it would run, and runs and records nothing.
  - It is planned as a check would be, from master's own Portfiles in each of the check's environments, so a port master builds only with Xcode is unmet where there is none. It builds the ports it names, not the rest of their subports.
  - It rebuilds each port only in the environments where it failed, or its tests did, and reports it there. A port named with `--only` that failed nowhere is rebuilt everywhere the check built it.
  - A port that failed only before building, at lint, fetch, or checksum, is left out unless `--only` names it: those failures come from the branch's own Portfile and distfiles. So is a port the branch adds, since master has nothing to compare.
  - A failed check points to it when a baseline can answer something, naming the ports and the master: `To see whether jq fails at master 1a2b3c4 too: dockhand check --baseline --branch jq-update`.
  - Baselines are off by default. With `check.baseline = true`, a failed check runs the one it points to by itself.
- **`status`** lists the checks queued and running, **`wait check-12`** follows one until it ends, and **`cancel check-12`** stops one, keeping what finished. In a branch's worktree, `wait` and `cancel` with no check named take the branch's latest, as `logs` does.

## Where it builds

### tart

Each check clones one of dockhand's images for each release and attempt, and deletes the clone afterwards ([details](../tart-provider.md)). The images live in `~/.dockhand/tart`, or `$DOCKHAND_TART_HOME`, apart from your own Tart VMs.

```sh
dockhand setup tart               # this Mac's release
dockhand setup tart sequoia       # another; names like tahoe or numbers like 15
dockhand setup tart --xcode ~/Downloads/Xcode_26.xip   # the Xcode add-on for this Mac's release
dockhand setup tart --check       # check the image in a disposable clone
dockhand setup tart --rebuild     # a replacement, keeping the old one until the new one passes
```

An image starts from Cirrus Labs' vanilla macOS image, and holds the Command Line Tools of the release's pinned generation and MacPorts: this Mac's, never older than the 2.12.6 dockhand pins, unless `--macports-version` names another. Making one downloads the vanilla image the first time and takes up to 60 GB of disk. A golden copy is kept beside it, and a lost image is restored from it. Golden Gate, macOS 27, needs Tart 2.39.0 or newer, since its images have ASIF disks, which older Tart can't list while they run; setup says so before it starts anything.

Setup records what each image was made from: the vanilla image by digest, and the tools and MacPorts it installed. A check's results stand for the image it ran in. Once an image is made again from a newer vanilla image, or with other tools, what passed in the old one no longer counts, and `status` names the ports to check again. Rebuilding from the same source with the same tools keeps them.

Xcode is an add-on. `--xcode`, given an Xcode `.xip` from Apple or a folder of them, makes `dockhand-xcode-<release>`: the same image with Xcode too, in up to 65 GB more. Its Xcode is the one MacPorts' arm64 buildbot for the release runs, such as 26.6 on Tahoe, so ports build as MacPorts builds its packages. `providers.tart.xcode` names another for a release. Setup needs the archive of exactly that version, and says which to download when the folder lacks it; betas are never chosen. With `xcodes` installed (`sudo port install xcodes`), setup downloads it for you, signing in with your Apple ID through xcodes: at a terminal it asks first, and without one it uses the sign-in xcodes kept from the last time. When a release has an Xcode image, checks on it use it. A port that needs Xcode, itself or through a changed prerequisite, is built only there; without it, the port is unmet, not failed, and the check says to add the image.

macOS runs at most two VMs at once, your own among them, and that is Tart's only limit: two checks run at once, each VM waiting for a free one before it starts, so a `minmax` check beside another waits for its second VM. There is no setting for it; a configuration that still sets `providers.tart.capacity` is refused by name, so delete the line. `providers.tart.test_timeout` bounds each target's tests, 30 minutes by default, and `providers.tart.build_timeout` its build, its dependencies' installs, fetch, checksum, and install together, 6 hours by default, GitHub's own cap on a job; lint has 10 minutes. A build past its bound is ended, with what it started, and fails, said as having run past it. On GitHub, a run building past `providers.github.build_timeout`, 6 hours by default, is cancelled, and the port it was installing then fails, as one does whose job GitHub ended at its cap; a port whose install began and never ended is never read as passed.

### github

`--on github` builds with MacPorts' own CI workflow in your fork's GitHub Actions, the way a pull request is built ([details](../github-provider.md)). It needs the GitHub login, a remote that pushes to your fork, and Actions enabled on the fork, which GitHub turns off for new forks.

It pushes the commit to a `dockhand-check/` branch of your fork, and removes that branch once it has read the run, keeping the logs here. Where it can't, it says so, and `clean` removes it once your branch is merged.

### command

Your own script, for a build box or a VM you manage. Dockhand gives it a request file and reads back a result file. It can't vouch for how the script built, so results read "reported by <name>" ([details](../command-provider.md)).

```toml
[providers.command]
run = "~/bin/build-ports"
name = "buildbox"
```
