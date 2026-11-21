# Using Dockhand

Dockhand's long-form workflow guide is embedded in the binary. Run
`dockhand usage` for first setup and the path from an upstream release to a pull
request; run `dockhand usage <topic>` for a complete workflow or a particular
set of choices. The same pages are linked below.

`dockhand <command> --help` is the quick command reference. `dockhand config`
shows the configuration selected by the invocation. An agent with only the
binary can read every topic without a Dockhand source checkout.

| Page | Command | Use it to |
| --- | --- | --- |
| [From first run to a pull request](usage/setup.md) | `dockhand usage` | First setup and a release update through to its pull request |
| [Branches and worktrees](usage/branches.md) | `dockhand usage branches` | Find, start, adopt, rebase, restore, and retire work |
| [Editing by hand](usage/hand-edits.md) | `dockhand usage hand-edits` | Edit Portfiles, refresh checksums, and request rebuilds |
| [Builds and tests](usage/checks.md) | `dockhand usage checks` | Choose files, targets, tests, and build environments |
| [Publishing and review updates](usage/publishing.md) | `dockhand usage publishing` | Shape commits, assess evidence, and submit or update a PR |
| [Recovery and diagnosis](usage/recovery.md) | `dockhand usage recovery` | Diagnose holds, failed checks, interruptions, and stale evidence |
| [Many ports at once](usage/batches.md) | `dockhand usage batches` | Select many ports and manage partially completed work |
| [Unattended work](usage/unattended.md) | `dockhand usage unattended` | Bump, background checks, serve, and unattended holds |
| [A new port](usage/new-port.md) | `dockhand usage new-port` | Create and finish a Portfile for a new project |
| [Reviewing pull requests](usage/review.md) | `dockhand usage review` | Inspect, adopt, check, and review another pull request |
| [Scripts and agents](usage/agents.md) | `dockhand usage agents` | Explicit context, JSON, exit codes, scratch state, and automation |
| [Configuration](usage/configuration.md) | `dockhand usage configuration` | Profiles, holds, service settings, paths, and retention |

Start with the default guide, then choose the workflow you need. Each page
states its starting context, shows commands with expected outcomes, and names
related topics using commands that also work from the binary. Topic names are
stable entry points, not a second listing of flags.

[Design v3](design-v3.md) explains the model, [architecture](architecture.md)
maps it to code, and [decisions](decisions.md) records the standing choices.
Those are repository references; the usage pages supply the context needed for
their workflows directly.

## Where checks build

Dockhand runs lint, fetch, checksum, installation, and declared tests. Test
failures are advisory by default, matching MacPorts CI; `--tests required`
makes failed or timed-out declared tests fail the check.

| Build environment | How it works |
| --- | --- |
| **Tart** | Builds in disposable macOS VMs on your Apple silicon Mac. The usual `minmax` profile selects VMs for each port's oldest and newest release you have images for. See [the Tart provider](tart-provider.md). |
| **Local** | Builds in a MacPorts prefix on this Mac, such as `/opt/local`, alongside your active ports. Afterward, Dockhand restores the prefix; `--keep` leaves the branch's versions active. Select it with `--on local` or `--on local:/opt/macports-test`. See [the local provider](local-provider.md). |
| **GitHub Actions** | Runs MacPorts' own workflow in your fork. Enable Actions in the fork and select `--on github`. See [the GitHub provider](github-provider.md). |
| **Custom command** | Runs a script you configure for a build machine or VM you manage. See [the command provider](command-provider.md). |

`dockhand setup tart sequoia` prepares another release's image, and
`dockhand check --build macports-ci` checks on the releases MacPorts' CI
workflow names. Ports requiring full Xcode need an image prepared with
`dockhand setup tart --xcode <archive.xip>`. `dockhand usage checks` has the
rest.

## More port work

| Task | Command |
| --- | --- |
| Start a manual fix | `dockhand edit ripgrep --new=ripgrep-fix` |
| Bring an existing branch into Dockhand | `dockhand adopt` from that branch's worktree |
| Draft a port from a GitHub project | `dockhand create <github-url> --new --category devel` |
| Find affected dependents | `dockhand impact` |
| Preview library rebuilds for an update | `dockhand update <port> --revbump-dependents --plan` |
| Compare upstream source archives | `dockhand diff --source` |
| Refresh the branch against upstream master | `dockhand rebase` |
| Undo the latest tidy or rebase, or put back a prefix a local check changed | `dockhand undo` |
| Review someone else's pull request | `dockhand review <pr>`, or `dockhand review <pr> --check` to build it in Tart |
| Keep a check's VM and open a shell in it | `dockhand check --keep`, then `dockhand shell check-12` |
| List and update the ports you maintain | `dockhand outdated --mine`, then `dockhand ship --mine --to check` |
| Process checks, reviews, and new releases in the background | `dockhand serve --install` |
| Watch every branch in one table | `dockhand console` |
| Remove merged branches and their worktrees | `dockhand clean` |

New Portfiles include `# dockhand: unconfirmed` markers for inferred values.
Review and correct those fields, then remove the markers before submitting. For
Go and Rust version updates, prefer `update` over editing the version and
running `checksums`: `checksums` alone does not regenerate dependency lists.

## What Dockhand covers

Dockhand is built for routine port maintenance, and is used for it every day:

- **Version updates from GitHub releases**, and from tags on GitLab.com or a
  self-hosted GitLab. Go and Rust ports, with `go.vendors` and `cargo.crates`
  regenerated, are the most exercised; C and C++ ports building with CMake,
  Meson, or autotools are updated the same way, such as fluent-bit, cglm,
  wasm3, and exiv2.
- **Many ports at once.** Your own ports, or any selection by maintainer or
  category, through `outdated`, `ship`, and `serve`, under the same holds.
- **Library updates** with `--revbump-dependents`, such as libuv's.
- **New ports** with `create`, from a GitHub URL, `pypi:`, `crates:`, or `go:`.
- **Revision bumps** with `revbump`.

Some ports get a correct plan or a clear refusal rather than an update:
subports sharing one Portfile (py-lmdb, terraform-1.16), Python ports,
upstreams hosted outside GitHub and GitLab, Git-fetched ports, and Portfiles
that compute their version in ways the evaluation can't follow. A refusal says
what Dockhand couldn't do and the manual steps available. Read the plan and
diff of anything outside the list above more closely.

A pull request someone else wrote, which `review` reads and `adopt --pr` brings
into a branch, holds their Portfiles. When a command you run evaluates them on
your Mac, it says so once, naming whose they are: "Evaluating
`@example-user`'s Portfiles on this Mac, for #35174". A local check of an
adopted branch, which would run their Portfiles as root, is refused, naming
`--on tart` and `--on github` instead; `review <pr> --check` builds it in Tart.

## Install details

Dockhand needs MacPorts on your Mac even when it builds elsewhere: it uses
MacPorts to evaluate Portfiles. Some Go and Rust ports also need the MacPorts
`go2port` or `cargo2port` helper to regenerate their dependency lists. Dockhand
finds `tart`, `go2port`, and `cargo2port` as it finds `port-tclsh`: on your
`PATH`, then in `/opt/local/bin`, unless `--tool`, a `*_BIN` variable, or
`[tools]` names one.

Installed with MacPorts, Dockhand's completion scripts for zsh, bash, and fish
are already in place under `/opt/local/share`. Fish needs nothing more; Apple's
`/bin/zsh` needs `/opt/local/share/zsh/site-functions` on its `fpath`; and bash
needs the `bash-completion` port. The help of `dockhand completion zsh`,
`completion bash`, and `completion fish` says how, for a build from source too.

Dockhand saves its GitHub login in the macOS Keychain; `GH_TOKEN`,
`GITHUB_TOKEN`, or GitHub CLI credentials also work, and environment tokens take
precedence. This API login is separate from Git's authentication for pushing
commits. `dockhand usage` covers the Keychain over SSH and the rest of setup.
