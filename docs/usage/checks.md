# Build and test a branch

Use this page to choose what a check captures, which ports it builds, where it
builds them, and how to inspect the result. A check records evidence for
particular files and environments. It does not commit the branch or publish a
pull request.

For a first-time installation, start with `dockhand usage`. For a failed or
interrupted check, see `dockhand usage recovery`.

## Start with the files you mean to check

Enter the branch's worktree before checking uncommitted work:

```sh
cd "$(dockhand path BRANCH)"
dockhand check --plan
dockhand check
```

Replace `BRANCH` with its exact name from `dockhand status`. The plan shows the
captured files, targets, build order, and environments. The real check normally
captures tracked working files, including uncommitted edits, as a numbered
snapshot. Deterministic lint fixes may be made before capture so the checked
files match what `tidy` would commit.

Use a capture flag when you mean another state:

| Command in the worktree | Files checked |
| --- | --- |
| `dockhand check` | Tracked working files as they are now |
| `dockhand check --staged` | The Git index |
| `dockhand check --head` | The committed tip |
| `dockhand check --include files/patch-example.diff` | Working files plus that untracked file, without staging it |

Untracked files are otherwise excluded and reported. Stage a new Portfile or
patch with Git when it should belong to the branch, rather than relying on
`--include` for every check. A tracked `patchfiles` entry whose required patch
is absent from the captured files is refused.

From another directory, `-b BRANCH` selects the branch. A branch with both
commits and working edits may require you to choose `--head` or
`--working-tree`; a named branch's plain check is not a promise to capture every
uncommitted file. You can be explicit:

```sh
dockhand check -b BRANCH --working-tree --plan
dockhand check -b BRANCH --working-tree
```

## Understand targets and test policy

A changed Portfile or a change under its `files/` directory selects that
directory under MacPorts CI's rules. Its eligible subports are separate targets.
Each environment evaluates them for itself, omits unsupported or otherwise
excluded targets with reasons, and uses its own dependency order.

For each target, the build follows MacPorts CI's stages: lint, dependency
installation, fetch, checksum, install from source, then declared tests. Tart
builds with the target's dependencies active; a local prefix normally builds
among its existing active ports.

Tests have three policies:

- `declared`, the default: run declared tests and report their outcome; failing
  tests are advisory.
- `required`: declared tests that fail or time out fail the check.
- `skip`: do not run them where Dockhand controls the build. GitHub's workflow
  still runs its own tests, but their result does not count under this policy.

A port that declares no tests can pass under any policy; `required` does not
invent a test suite. To require tests for this check:

```sh
dockhand check --tests required
```

Narrow or extend the targets when you have a reason:

```sh
dockhand check --only jq
dockhand check --also DEPENDENT
dockhand check --only jq --variants '+tests -docs'
dockhand check --only jq --variants each --plan
```

`--only` keeps selected changed targets and adds back changed prerequisites they
need. `--also` builds an unchanged port against the branch. Replace `DEPENDENT`
with an actual port named by `dockhand impact`. A narrow pass does not prove the
omitted targets passed.

`--variants each` builds the selected port with defaults, then each eligible
non-default variant separately; `universal` is excluded. It multiplies work
across environments. More than 12 builds requires confirmation, or `--yes`
without a terminal. GitHub checks do not accept variant selection because its
workflow controls variants.

## Choose build environments explicitly

`--build` selects a configured profile; `--on` selects explicit environments.
They are mutually exclusive.

```sh
dockhand check --build minmax --plan
dockhand check --on tart:sequoia --on tart:tahoe
dockhand check --on github
dockhand check --on local
```

Every requested environment contributes its own result. A pass on one does not
cover another. The submission profile can require environments beyond a
diagnostic check you ran manually.

| Profile | What it selects |
| --- | --- |
| `host` | A Tart image of this Mac's macOS release |
| `minmax` | Each port's oldest and newest eligible releases with available Tart images, macOS 12 or later |
| `macports-ci` | Tart releases selected from MacPorts' workflow at master |
| `all` | Every available Tart release |

`build.default` selects ordinary checks; a configured command provider becomes
the fallback default instead of `host`. `build.unattended` selects checks on
unattended publication paths such as `bump` and `serve`; otherwise its fallback
is `minmax` when Tart is available, or the ordinary default. Use
`dockhand config` and the check plan to see which configuration is in effect.
Define your own profile with `dockhand usage configuration`.

Use `--on local` for a host MacPorts prefix; `local` is no build profile, and
`--build local` is refused.

Where you name where a check builds, with `--on` or `--build`, submission needs
exactly that: `check --on local` then `submit` passes on the prefix alone, even
where Tart is set up. Where you name neither, it needs `build.default`'s
environments as well as the check's own. The newest check of the files that
covers every changed port decides, so asking again replaces an earlier ask; a
check narrowed with `--only` adds its results but sets no ask. A failure in any
check of the same files still blocks until it passes or is accepted, while its
environment stands.

### Tart guests

Tart requires Apple silicon. Each execution clones a prepared image and runs the
shared build program inside it. Ordinary checks remove the clone afterward; the
prepared images remain available for later checks.

```sh
dockhand setup tart
dockhand setup tart sequoia
dockhand setup tart --check
```

The first command prepares this Mac's release; the second prepares another
release; the third validates the existing image in a disposable clone. Setup
downloads macOS and can use tens of gigabytes. Dockhand keeps its images under
`~/.dockhand/tart`, separate from your own Tart home.

Ports that need full Xcode are unmet in an image without it. Add the exact Xcode
version setup requests, using an archive or directory of archives:

```sh
dockhand setup tart --xcode ~/Downloads/Xcode.xip
```

`--rebuild` replaces an image after validating its replacement. Changes to an
image's source or tools can invalidate earlier evidence; check the affected
targets again. macOS's VM limit includes your other running VMs, so queued work
can wait for a slot without having failed.

### What a check installs

check's plan estimates what each environment installs before it builds,
`Installs    about 14 dependencies to install, 3 with no archive on
packages.macports.org for this release, by the port index; the clone decides`.
Those with no archive build from source, which can take a while; it's a hint
for choosing `-d`, and the clone decides. `check --plan` looks the archives up;
a check says them as it starts.

### GitHub Actions

`--on github` needs a login, a Git remote that pushes to your fork, and Actions
enabled there. It pushes a `dockhand-check/` branch and runs MacPorts' own
workflow. Dockhand reads the results and keeps logs locally, then removes the
temporary branch when it is done with it. This has external effects even though
it opens no PR.

Each macOS release in the workflow's matrix is its own environment, with its own
column, and one run builds them all. `--on github` asks for every release;
`--on github:15` is refused, so check one release with Tart or `local`. A
release whose job no runner took is left unchecked, and submit says so with
GitHub's reason; check that release with Tart or `local`, or try again later.
The workflow builds only the ports a commit changes, so the plan says which
`--also` or baseline targets it won't build.

The workflow chooses its runners and tests. A host check's pass does not
guarantee those runners will pass. `dockhand usage publishing` explains local
evidence and MacPorts CI on the eventual pull request.

### A local MacPorts prefix

`--on local` uses `/opt/local` unless configured otherwise.
`--on local:/opt/macports-test` selects an existing installation at that path.
Dockhand does not create a MacPorts installation. The prefix must have MacPorts
2.9 or newer for snapshot and restore support. Published MacPorts binary
archives target `/opt/local`; in another prefix, dependencies build from source.

A local check snapshots active ports, builds using the staged branch first in
its source configuration, then restores the previous active versions. Its
default environment includes ports already active on the Mac. A configured
dedicated prefix can be cleaned between targets with `providers.local.clean`; do
not treat a pass among everyday active ports as the same experiment as a clean
guest.

If the registry requires root, the check needs `sudo`; a foreground terminal can
ask for the password. `--keep` leaves the branch's versions active, and
`dockhand undo check-N` puts the prefix back as it was, each port active before
active again at its version; MacPorts' `port restore` brings back only the ports
requested before and what they need. A check that stopped before it could put
the prefix back is put back the same way, or by the next check there; a check of
the same branch first needs `dockhand wait`, `check --replace`, or
`dockhand cancel` of the stopped one. Restoring active versions leaves
newly built images inactive and unrequested for MacPorts' `port reclaim`. A
later check can remove a matching inactive, unrequested image before rebuilding;
an installed image it cannot safely replace is refused.

An adopted pull request cannot use `local`. Its Portfiles belong to someone
else, and building them would execute them on this Mac. Use Tart or GitHub
instead. Local checks under `serve` are disabled unless explicitly configured.

### Your build script

`--on command` invokes the script configured under `[providers.command]`.
Dockhand supplies a request file and expects a result file; it cannot establish
how your script built the ports. Results identify the reporting script. This
provider is for a build environment you already manage, not an automatic
fallback when Tart is unavailable.

## Run in the foreground or queue it

Without `serve`, an ordinary check runs here. Ctrl-C stops that work, preserving
completed results. With `serve` leading, it runs the check and this command
follows; Ctrl-C then stops following, not the service's check.

```sh
dockhand check -d
dockhand wait
dockhand status
```

`-d` queues and returns. If no service is leading, Dockhand starts a background
`serve --drain` worker, whose log is `drain.log` beside the database. `wait`
follows the latest check of the current branch. The output also names the check,
such as `check-12`, for use from anywhere.

One branch runs one check at a time. A new request follows an active check only
if it covers the requested files, targets, environments, and policy. Otherwise
it refuses. `--replace` stops the active request, keeps completed results, and
queues the new one. Read `dockhand usage recovery` before choosing between wait,
retry, replacement, and cancellation.

## Read results and reuse

| Result | What to do |
| --- | --- |
| `passed` | Inspect test policy, notices, and environment coverage before submitting |
| `failed` | Read the named port's failed phase and log |
| `blocked` | Fix the changed prerequisite that prevented this target building |
| `unmet` | Provide the requested environment capability, such as Xcode, or choose a suitable environment explicitly |
| Not run or interrupted | Determine whether work is still queued, resumable, excluded by plan, or needs another check |

```sh
dockhand logs check-12
dockhand logs check-12 jq
dockhand logs check-12 jq --phase install
dockhand artifacts
```

Use the actual check and port names. Logs identify the build when several
environments exist; `--full` includes dependency installation, and `--follow`
follows supported live logs. `artifacts` lists kept archives of the branch's
latest check, or explains why one is missing.

Tart results can be reused when recorded input content and environment identity
still match, including dependencies and fetched Git commits. `--fresh` requests
a rebuild. A rebase can preserve valid evidence when those inputs did not
change. Results from separate checks can jointly cover the same branch files; a
narrow rerun does not erase other still-valid results. Moved tags or changed
inputs can make evidence stale even when a version string did not change.

To retain a Tart build for inspection, request `--keep` before running it:

```sh
dockhand check --keep
dockhand shell check-12 -- otool -L /opt/local/bin/PROGRAM
```

Replace the identifiers with the check and installed program. `shell --test` can
try edits to the test phase in a retained clone, but that experiment is not
recorded check evidence. See `dockhand usage recovery` for baselines and
debugging, and `dockhand usage publishing` for the evidence required to submit.
