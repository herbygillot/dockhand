# From first run to a pull request

Dockhand prepares MacPorts changes, checks them, and opens or updates their pull
requests. Use this page for the first run and the ordinary path from an upstream
release to a PR. The guide is embedded in the binary: `dockhand usage <topic>`
prints a complete workflow. `dockhand <command> --help` gives that command's
flags.

If you are an agent starting with only the binary, read `dockhand usage agents`
before changing an existing workspace. It explains invocation context,
machine-readable results, and unattended behavior.

## Know what Dockhand manages

Dockhand works in a clone of `macports/macports-ports`. Each change has an
ordinary Git branch, usually `dockhand/<name>`, and a separate working directory
called a worktree. A branch can change several ports and has at most one pull
request. Dockhand records its edits and checks alongside Git; the database does
not replace the files or commits.

The usual sequence is:

| Stage | What it leaves |
| --- | --- |
| `update` | Edited Portfiles, checksums, and supported dependency lists; no commit |
| `tidy` | Commits suitable for MacPorts review |
| `check` | Build results for captured files in selected environments |
| `submit` | A push to your fork and a pull request on MacPorts |

`ship` runs those stages together. A preview uses `--plan`; it does not perform
the proposed edit, build, or submission. It can still read the network and local
records to prepare the preview.

## Set up a checkout once

You need Git 2.40 or newer, a MacPorts installation, and a ports checkout. To
publish, you also need a fork of `macports/macports-ports` and a GitHub login
that can push to it. Dockhand does not install MacPorts for you.

If you already work on MacPorts, enter that checkout and keep its existing
remotes. Otherwise, fork the repository on GitHub, replace `YOUR_GITHUB_LOGIN`
below, and clone your fork:

```sh
git clone https://github.com/YOUR_GITHUB_LOGIN/macports-ports.git \
  ~/Source/macports-ports
cd ~/Source/macports-ports
git remote add upstream https://github.com/macports/macports-ports.git
dockhand setup
```

`setup` checks the tools, registers the checkout, and reports the GitHub login
and build environments it can use. On a terminal it offers missing setup steps.
You can decline each offer. Creating a Tart image downloads macOS and is a
separate choice. `dockhand setup -y` skips the offers; it does not silently
complete every missing dependency.

Branch worktrees go under `~/Source/macports-branches` by default. To choose
another location during setup:

```sh
dockhand setup --worktrees ~/Source/port-work
```

Commands use `--tree`, then `MACPORTS_TREE`, then the current directory to find
the ports checkout. Inside one of its worktrees, Dockhand also knows which
branch is checked out. `dockhand usage branches` explains branch selection in
detail.

## Sign in and choose somewhere to build

If setup did not establish a login:

```sh
dockhand setup github
```

It uses a browser code and stores Dockhand's login in the macOS Keychain.
`--no-browser` prints the address instead. `GH_TOKEN` or `GITHUB_TOKEN` takes
precedence over a stored login; an available GitHub CLI login is another
fallback. `dockhand setup` shows the account and credential source. Git pushing
must also work through the fork's configured Git transport.

Choose a build environment before the first real change:

- **Tart on Apple silicon:** install Tart if needed, then run
  `dockhand setup tart`. This prepares an image of this Mac's release. The
  default `host` build profile uses a Tart guest; it does not install the port
  into your everyday prefix.
- **GitHub Actions:** enable Actions in your fork. `--on github` pushes a
  temporary check branch and runs MacPorts' workflow there.
- **A MacPorts prefix on this Mac:** `--on local` builds among the active ports
  in the configured prefix, `/opt/local` by default. It changes that
  installation during the check and restores its active versions afterward. Read
  `dockhand usage checks` before choosing it.

For a persistent default other than Tart, define a profile. For example, add
this to `~/.dockhand/config.toml` to use your fork's Actions:

```toml
[build]
default = "fork-ci"
unattended = "fork-ci"

[build.profiles]
fork-ci = { provider = "github" }
```

Merge examples into an existing file rather than repeating TOML table headers.
`dockhand config` shows the settings it read. See `dockhand usage configuration`
for profiles, paths, and other defaults.

## Take an existing port through the whole workflow

From your registered checkout, choose a port with a newer upstream release.
These examples use `jq`; substitute your port.

```sh
dockhand status
dockhand ship jq --plan
dockhand ship jq
```

The plan identifies the change and branch. The real run updates the port, shapes
its commits, checks it, then pushes and opens or updates its PR. On a terminal,
`ship` asks once before proceeding. Without a terminal, invoking `ship` is
consent to proceed under its unattended holds; `--yes` has that behavior too.

Watch the stage results. A hold, failed build, or unsupported edit can stop the
branch before submission. Completed work is retained. Inspect the named branch
with `dockhand status -b BRANCH`, follow its printed next action, and use
`dockhand usage recovery` when the reason is unfamiliar. A second
`ship -b BRANCH` resumes that branch instead of starting its work again.

Use `--to commit` or `--to check` when you want `ship` to stop before
publishing. If you explicitly choose a provider, for example
`dockhand ship jq --on github`, its check builds there.

## Inspect each stage yourself

For a change you want to review before committing, use the stages directly:

```sh
dockhand update jq --new
dockhand path -p jq
```

The update prints the exact branch name and changed files. Enter its worktree;
when several branches involve jq, use the exact printed name with
`dockhand path BRANCH` instead of `-p jq`:

```sh
cd "$(dockhand path -p jq)"
dockhand diff
dockhand impact
dockhand tidy --plan
dockhand tidy
dockhand check --head
dockhand submit --plan
dockhand submit
```

`diff` shows the change; `impact` shows the changed ports and dependents worth
checking. `tidy` makes the commits. `check --head` checks those committed files.
`submit --plan` now previews the actual submission, including the check evidence
and holds. `submit` performs the push and PR operation. If you edit files after
checking, check the resulting files again; eligible unchanged builds can be
reused.

This worktree context matters: a plain `check` there captures working files,
whereas naming a branch from elsewhere can select its committed head or require
an explicit capture choice. See `dockhand usage checks` for working, staged, and
committed captures.

## Follow the PR and finish the branch

```sh
dockhand status --refresh
dockhand open -p jq
```

Status shows reviews, CI, and what needs you. `open` opens the PR when there is
one. Make requested changes in the same branch, tidy and check them, then submit
again. See `dockhand usage publishing` for amending, rebasing, drafts, and
accepted failures.

After a merge, inspect cleanup before removing branch worktrees and other
eligible leftovers:

```sh
dockhand clean --plan
dockhand clean
```

The real cleanup asks on a terminal; a script must explicitly request the
removal with `--yes`. Read the plan first: a worktree it removes goes with
its ignored files, which the plan doesn't list.

## Choose the next workflow

| Read | When you need it |
| --- | --- |
| `dockhand usage branches` | Find, start, adopt, rebase, restore, or retire a branch |
| `dockhand usage hand-edits` | Edit a Portfile yourself, refresh checksums, or rebuild dependents |
| `dockhand usage checks` | Choose files, targets, tests, build environments, and retained guests |
| `dockhand usage publishing` | Shape commits, assess readiness, submit, and respond to reviews |
| `dockhand usage recovery` | Diagnose a hold, failed check, interruption, or stale result |
| `dockhand usage batches` | Select and update many ports without losing track of partial work |
| `dockhand usage unattended` | Run `bump`, background checks, or the persistent `serve` process |
| `dockhand usage new-port` | Create and finish a Portfile for a project MacPorts does not have |
| `dockhand usage review` | Inspect or check someone else's pull request |
| `dockhand usage agents` | Use structured output, explicit context, and isolated scratch state |
| `dockhand usage configuration` | Set profiles, holds, service defaults, paths, and retention |
