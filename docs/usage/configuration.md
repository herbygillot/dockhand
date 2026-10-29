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

The report names the configuration file, database, and effective settings. The
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
ordinary checks use `host`, or `command` when a command provider is configured.
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
```

These bound tests and build work per target. Lint has its own 10-minute bound.
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
capacity = 2
build_timeout = "6h"
```

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
root also needs noninteractive sudo access; that allows Portfiles to execute as
root on this Mac. It does not permit local checks of adopted pull requests. Read
`dockhand usage checks` before enabling it.

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
itself.

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
be compared remain a hold. An unrecognized rule is refused with the known rule
names.

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

## Paths and limits worth knowing

The main path selections are `MACPORTS_TREE`, `DOCKHAND_DB`, and
`DOCKHAND_CONFIG`; `--tree` and `--db` override the first two. Worktrees have
their own `worktrees` setting. A scratch checkout alone does not isolate the
other three locations. `dockhand usage agents` gives a complete scratch example
and environment reference.

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
