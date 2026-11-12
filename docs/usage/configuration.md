# Configure Dockhand for your workflow

Use configuration for choices you want repeated: worktree locations, build
environments, test policy, unattended holds, and service behavior. Use command
flags for a one-off choice. For an isolated experiment, read
`dockhand usage agents` before changing paths or opening another build's
database.

## Find and validate the file

```sh
dockhand config
dockhand config --json
```

The report names the configuration file, database, and effective settings, each
marked `(default)` unless the file sets it; a value the file sets is the file's
even where it equals the default, an explicit `false` included. The
default file is `~/.dockhand/config.toml`; `DOCKHAND_CONFIG` selects another. An
absent file is allowed. An unknown key or malformed value is refused rather than
silently ignored.

Edit the TOML file with your editor, then run `dockhand config` again. In an
existing branch's worktree, use `dockhand check --plan` to verify that its
resolved build environments and tests are what you intended. A running or queued
check keeps the plan accepted for it; editing the file does not rewrite that
check's plan.

Flags override corresponding environment selections, then file settings, then
defaults. This is precedence for supported choices, not a naming scheme:
arbitrary configuration keys do not automatically have a flag or environment
variable. For example, `--tree` overrides `MACPORTS_TREE`; `DOCKHAND_CONFIG`
chooses the file itself.

## Start with a small file

Set your own maintainer entry and worktree directory before TOML section
headers. Replace the example identity with the spelling appropriate for your
MacPorts ports:

```toml
maintainer = "{example.org:you @you} openmaintainer"
worktrees = "~/Source/macports-branches"

[check]
tests = "declared"
baseline = false

[build]
default = "host"
unattended = "minmax"
```

`maintainer` identifies your ports for `--mine` and the service's default
selection, and supplies new ports' maintainers line. On a terminal,
`dockhand setup` offers the line the ports naming your GitHub account write,
and saves it here if you say yes. Git commit author
identity and GitHub authentication are separate from this setting.

The `host` and `minmax` profiles in this example use Tart. On a machine without
Tart, choose an appropriate profile rather than copying those defaults
unchanged.

## Define where builds run

A profile names a provider and, where that provider takes releases, a list or
release-selection rule:

```toml
[build]
default = "two-releases"
unattended = "two-releases"

[build.profiles]
two-releases = { provider = "tart", releases = ["sequoia", "tahoe"] }
fork-ci = { provider = "github" }
```

Use `dockhand check --build two-releases` for that profile, or
`dockhand check --on github` to bypass profile selection for one request.
`--build` and `--on` cannot be combined. Every selected environment matters to
the check's evidence.

The built-in Tart profiles are `host`, `minmax`, `macports-ci`, and `all`.
`minmax` selects each port's oldest and newest eligible available releases, with
macOS 12 as its floor. `macports-ci` reads the releases from MacPorts' workflow
at master; it is not a permanently fixed release list. With no explicit default,
ordinary checks use `minmax`, which is this Mac's release alone where only its
image is set up, or `command` when a command provider is configured.
Unattended publication paths fall back to `minmax` when Tart is available,
otherwise the ordinary default.

`--on local` means the MacPorts prefix on this Mac, and Dockhand never chooses it
on its own. `local` is no build profile: `--build local` and
`build.default = "local"` are refused, naming `host` for Tart on this Mac's
release and `--on local` for the prefix. Custom profiles currently support `tart`, `github`, and `command`; they do not
accept `provider = "local"`. Select a host prefix explicitly with `--on local`
or `--on local:/path/to/prefix`.

If you merge these examples into one file, keep only one `[build]` header and
combine its keys. Repeating a TOML table declaration is invalid.

## Provider settings

### Tart

```toml
[providers.tart]
test_timeout = "30m"
build_timeout = "6h"
stall_timeout = "30m"  # a target's log growing by nothing this long; "0" never
keep_failed = true   # keep a failed check's clone for dockhand shell
```

These bound tests and build work per target. Lint has its own 10-minute bound.
A target whose log grows by nothing for `stall_timeout` is taken as stalled: the
attempt ends as infrastructure trouble and is tried again, where a hung guest
once held a Tart slot for the six hours of `build_timeout`. The check says so
at half of it, as "libharbor's log has grown by nothing for 15m; at 30m its
build is taken as stalled".
A target ended at a bound says which, and the key that set it, as "the tests
ran past their 30m bound (providers.tart.test_timeout), so they were ended",
with its log's last lines; a local check names providers.local's.
`providers.tart.xcode.<release>` can select another Xcode version for a release;
otherwise setup matches MacPorts' arm64 builder. Changing that setting describes
a future image setup, not an in-place tool replacement in every existing image.
The image setup command's help describes its options; `dockhand usage checks`
covers image creation and validation.

Tart has no configurable capacity count. Dockhand waits for the Mac's VM
capacity, including your other running VMs.

### GitHub Actions

```toml
[providers.github]
capacity = 20
build_timeout = "6h"
```

GitHub queues runs past your account's own job limits, and queued time counts
against no build timeout, so `capacity` isn't a concurrency limit. It guards
dockhand's use of your hourly API budget: runs in flight share one listing of
the fork's workflow runs each poll. The default is 20.

`remote` selects a Git remote when more than one pushes to forks you own; omit
it when the choice is unambiguous. Enable Actions in the fork and establish a
login. Dockhand cannot raise GitHub's own job-time limits by setting a longer
timeout.

### A local MacPorts prefix

```toml
[providers.local]
prefix = "/opt/local"
keep = false
serve = false
test_timeout = "30m"
build_timeout = "6h"
```

Local checks change active ports during the build and normally restore their
earlier active versions. `keep = true` leaves the branch's versions active after
every local check, as `check --keep` does for one check. Inactive images can
remain for `port reclaim`.

`clean` is a list of dedicated prefix paths Dockhand may treat as its own, for
example `clean = ["/opt/macports-test"]`. There it deactivates unrelated ports
between targets as it does in guests. Do not put an everyday shared prefix in
that list merely to make a build pass.

`serve = true` permits the service to run local checks. A registry requiring
root also needs sudo to run the prefix's `port-tclsh` without a password, which
one sudoers line allows, added with `visudo`:

```text
you ALL=(root) NOPASSWD: /opt/local/bin/port-tclsh
```

Every root step of a local check runs that one command. It allows Portfiles to
execute as root on this Mac, as a rule for `port` does, and is no narrower:
`port-tclsh` runs whatever script it is given, from a directory you can write.
A prefix MacPorts installed without root needs no sudo at all, at the cost of
MacPorts' binary archives. serve's refusal names the line, with your name and
the prefix's `port-tclsh`; Dockhand never writes sudoers itself. It does not
permit local checks of adopted pull requests. Read `dockhand usage checks`
before enabling it.

### A command provider

```toml
[providers.command]
run = "~/bin/build-ports"
name = "buildbox"
capacity = 1
```

The script must implement Dockhand's request/result protocol. Its results are
labeled as reported by that script. A configured command provider is useful for
an environment you already manage; the example is not a working builder by
itself. docs/command-provider.md, in dockhand's repository, describes the
request and result files, and gives a script that builds on another Mac over
SSH.

## Set unattended policy deliberately

```toml
[holds]
majors = "hold"
allow = []

[holds.ports."harfbuzz"]
after = ["harfbuzz-devel"]
```

`majors = "hold"` is the default for updates nobody reviews. `"allow"` permits
major-version updates on those paths. `holds.allow` lists specific rule names
whose concerns remain visible but no longer hold unattended work. It does not
accept failed builds or waive every submission blocker. Archives that could not
be compared remain a hold. `link-undeclared` holds only where tidy couldn't
write the declaration a check's link finding names; elsewhere tidy writes it.
An unrecognized rule is refused with the known rule
names; the rule for a GitHub port's source is `source`, and its earlier name,
`github-tarball`, is refused like any other. `not-distributable`, a change that
makes a port one MacPorts' builders keep no archive of, is among them.

The `holds.ports."<port>"` table can set that port's `majors` and `allow`,
overriding global choices where supplied. It also accepts:

- `moves = "follow"`: follow a repository move on unattended updates; the
  default is `"hold"`. It never follows a port rename, a rename of the project
  the port is named after, which stays held for `dockhand rename`.
- `after = ["OTHER_PORT"]`: wait until each named port is at the same version on
  master or has an open PR for that version.

Use `dockhand usage unattended` for the hold workflow and
`dockhand usage publishing` for decisions on particular findings. `--yes`, a
finding decision, `submit --accept`, and a persistent `holds.allow` rule have
different scopes.

## Configure the service separately from ordinary commands

```toml
[serve]
ship_to = "list"
outdated_at = "07:00"
submit_limit = 10
notify = true
```

`ship_to` is `list`, `commit`, `check`, or `submit`. The default only lists
available updates; it does not open PRs. `outdated_at` is local time.
`submit_limit` bounds automatic PR creation per day when submission is enabled.

`maintainers`, `categories`, and `not_maintainers` select the ports serve looks
up, using the same selection semantics as `outdated`. If neither maintainers nor
categories is supplied, the default is your configured maintainer entry. For
example, `categories = ["devel"]` selects that category instead of silently
adding it to your own ports.

`dockhand serve --install` installs a launchd agent with the invocation's
relevant path and environment selections. Reinstall after changing those
selections. Configuration is read when the process starts. Restart the process
after changing its settings; rerunning `serve --install` replaces and restarts
an installed agent. Accepted check plans remain frozen. Read
`dockhand usage unattended` for starting, stopping, and observing the service.

## Submission and cleanup defaults

| Key | Default | Meaning |
| --- | --- | --- |
| `check.tests` | `declared` | `declared`, `required`, or `skip`; see the checks topic |
| `check.baseline` | `false` | Automatically run an applicable baseline after a failed check; it still accepts no failure by itself |
| `update.keep_http` | `false` | Leave plain-HTTP `homepage`, `master_sites`, and `livecheck.url` links as they are, asking nothing; otherwise update, create, and a checksums refresh switch each that answers over HTTPS. `--keep-http` does the same for one command |
| `update.source` | `suggest` | What a version update does about a port on the github PortGroup: `suggest` leaves its source as it is, but for a `github.tarball_from tarball` port's move to GitHub's archive, and says, as a notice, where a release's own archive is the source; `choose` moves a `tarball` port to a release's own source archive where one is the source, else to GitHub's archive, and a port cloned with Git for its submodules to a release archive holding every one, else leaves it; `keep` moves nothing; `releases` or `archive` asks for that source and refuses where it doesn't qualify. `--source` on `update`, `ship`, or `bump` does the same for one command |
| `submit.rerequest_review` | `ask` | `ask`, `always`, or `never` after pushing fixes to a PR with changes requested |
| `cleanup.automatic` | `true` | Perform eligible background pruning |
| `cleanup.after` | `15d` | Retention window for eligible history, logs, and caches |
| `cleanup.min_free` | `30GB` | Trigger eligible cleanup when free space is low |

Cleanup preserves open work and evidence still in use. It is not a substitute
for inspecting a branch before removing its worktree. `dockhand clean --plan`
previews explicit cleanup; `dockhand usage branches` covers branch retirement.

Durations use values such as `30m`, `6h`, or, for retention, `15d`. Storage
sizes accept decimal units such as `30GB` and `1.5TB`, or binary units such as
`64GiB`. `30GB` means 30,000,000,000 bytes.

## Name the programs dockhand runs

Dockhand runs nine programs that you can point at a particular copy:

| Program | Flag | Variable | `[tools]` key |
| --- | --- | --- | --- |
| git | `--tool git=…` | `GIT_BIN` | `git` |
| port-tclsh | `--tool port-tclsh=…` | `PORT_TCLSH_BIN` | `port_tclsh` |
| tart | `--tool tart=…` | `TART_BIN` | `tart` |
| go2port | `--tool go2port=…` | `GO2PORT_BIN` | `go2port` |
| cargo2port | `--tool cargo2port=…` | `CARGO2PORT_BIN` | `cargo2port` |
| xcodes | `--tool xcodes=…` | `XCODES_BIN` | `xcodes` |
| gh | `--tool gh=…` | `GH_BIN` | `gh` |
| patch | `--tool patch=…` | `PATCH_BIN` | `patch` |
| diff | `--tool diff=…` | `DIFF_BIN` | `diff` |

`--tool` comes first, then the variable, then `[tools]`, then the usual search
on `PATH` (and `/opt/local/bin` for MacPorts' programs). For example:

```toml
[tools]
git = "/opt/homebrew/bin/git"
go2port = "~/bin/go2port"
patch = "/opt/local/bin/gpatch"
```

A program named that isn't there refuses the command before anything runs,
naming where it was named: `GO2PORT_BIN: /opt/local/bin/go2port isn't
executable`. There is no fallback to `PATH`.

`--tool name=path` or a variable names a program for one command; `[tools]`
names it for every command and for serve. A value with a slash is the program's
file, absolute or under `~`; a bare name is found on `PATH`. `port` and
`portindex` follow `port_tclsh`; the local provider's prefix keeps its own.
`--git` is gone: use `--tool git=<path>`. `dockhand config` lists each
`tools.*` key with its value and where it came from.

## Paths and limits worth knowing

The main path selections are `MACPORTS_TREE`, `DOCKHAND_DB`,
`DOCKHAND_CONFIG`, and `[tools]`; `--tree` and `--db` override the first two. Worktrees have
their own `worktrees` setting. A scratch checkout alone does not isolate the
other three locations. `dockhand usage agents` gives a complete scratch example
and environment reference.

A database and the archives its checks kept are one store: the archives live
beside it, in `archives/` next to the database file. A copy of a database
that `DOCKHAND_DB` names takes that directory with it (on APFS, `cp -c -R`
clones it at no cost) and runs on a ports clone of its own; without it, the
copy's next check says its kept archives are missing, once, with their count,
and drops their records. A copy never reads its original's archives, whose
records a prune of the copy's would delete.

`dockhand backup <file>` makes a consistent copy of the database while serve
or a check writes to it: one SQLite file, readable only by you. Given a
directory, `dockhand backup ~/Backups`, the copy is named by the minute it was
made, `dockhand-2026-11-05-2114.db`; an existing file is never replaced. It
reads the database without migrating it, so a newer dockhand can back up a
database before its first migration. The copy is the database alone: kept
archives and check logs aren't in it, and a check run from it builds again
what it can't find, and says once which archives it dropped. For a whole copy,
stop serve and copy the directory, `cp -c -R ~/.dockhand <dir>` on APFS.

To put a backup back, stop serve (quit it, or `dockhand serve --uninstall`,
then `--install` after), move `dockhand.db`, `dockhand.db-wal`, and
`dockhand.db-shm` aside together, since a `-wal` left beside a put-back copy
would be replayed onto it, and copy the backup to `~/.dockhand/dockhand.db`.
The next command opens it, migrating it forward where the build is newer.

A newer dockhand migrates a database forward the first time it opens it, keeping
a copy of it as it was beside it, `dockhand.db.schema-<N>` for schema N, and an
older dockhand can't open it after. Where something already at that name isn't
a usable copy, such as an empty file, a directory or a link, nothing is
migrated, and dockhand names the path to move aside. A
development or rc build asks, on a terminal, before it migrates the default
database a release last opened; a copy named with `DOCKHAND_DB`, such as one
`dockhand backup` made, migrates without asking.

Credentials use `GH_TOKEN`, then `GITHUB_TOKEN`, then Dockhand's stored login,
then the GitHub CLI. Never put a token in the TOML file or a service command
line.

Limits can explain why a task stops or reports incomplete coverage:

| Boundary | Behavior |
| --- | --- |
| A comparison's source-file reading | Up to 1 MiB per file; a larger one is said, and holds, but for `go.mod`, `Cargo.toml`, `Cargo.lock`, and `package.json` |
| A project file read by `create` | Up to 4 MiB; a larger file stops creation with the file named |
| `go2port` or `cargo2port` | A 10-minute bound |
| A Portfile evaluation | A 5-minute bound |
| A `portindex` generation | A 1-hour bound; a full generation can take several minutes |
| Build infrastructure retries | Three attempts per environment; a build verdict is not automatically retried |
| A GitHub workflow appearing | A 10-minute wait before it is treated as infrastructure trouble |

Bounds are reported at the operation that reaches them. Before increasing a
configurable timeout, inspect whether the failure is a stalled environment, an
unavailable release, or a real build failure. `dockhand usage recovery` explains
how to distinguish them.
