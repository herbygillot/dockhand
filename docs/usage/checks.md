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

`--plan` evaluates every target with its variants in each environment and builds
nothing. With `-v` it prints that evaluation for each target in each
environment: the variants as MacPorts resolved them, the defaults not negated
plus those asked; `depends_build`, `depends_lib`, and `depends_run`; and
`configure.args` and `configure.env`, evaluated on this Mac with the
environment's platform and the developer tools of that release's image, as the
facts table records them; the guest's own build is the result, and a check says
where your image's tools differ from the table's. It is the dry evaluation of a Portfile under a variant
set, master's with `--also`, and `--json` carries it as `evaluated`.

The plan, and a check before it builds, say the Portfile findings submit would,
such as a line restating a PortGroup's default, so the edit comes before the
build it would otherwise repeat. A check says them once for each set of files;
`--json` carries them as `portfile_findings`.

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

The plan lists on `Changed` the subports the change alters, as dockhand's
evaluation of the base and the revision finds them, and on `Siblings` the
untouched subports of the same Portfile, which are built too because MacPorts
CI builds every subport of a changed Portfile. Where the subports can't be told
apart, for instance because the Portfile couldn't be evaluated, every subport is
listed on `Changed`, and the line below it says so and why.

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

Tests run for at most `providers.tart.test_timeout`, or
`providers.local.test_timeout` in a prefix, 30 minutes unless set. Tests ended
at that bound say so:
`the tests ran past their 30m bound (providers.tart.test_timeout), so they were
ended`.

Narrow or extend the targets when you have a reason:

```sh
dockhand check --only jq
dockhand check --also DEPENDENT
dockhand check --only jq --variants '+tests -docs'
dockhand check --only jq --variants each --plan
dockhand check --also curl --variants '+gnutls +sectrust'
dockhand check --plan -v --variants 'curl +gnutls +sectrust'
```

`--only` keeps selected changed targets and adds back changed prerequisites they
need. `--also` builds an unchanged port against the branch. Replace `DEPENDENT`
with an actual port named by `dockhand impact`. A narrow pass does not prove the
omitted targets passed.

`--variants` builds one port: the one named first in its value,
`'curl +gnutls +sectrust'`, a changed port or an `--also` one; with no name, the
one port the plan has, changed or `--also`; else the one `--only` names. Several
and none named is refused, naming them: `--variants builds one port; the plan
has curl and libpsl, so name it, as --variants 'curl +sectrust'`. Master's port
with chosen variants, an investigation rather than a change, is `--also curl
--variants '+gnutls +sectrust'` on a branch that changes nothing, started plain
with `dockhand start curl-sectrust`; `dockhand usage branches` says how such a
branch ends.

`--variants '+tests -docs'` builds the selected port once, in place of its
default build, with its default variants plus those given, as MacPorts opens
it; a `-variant` leaves a default out.
`--variants each`, or `'curl each'` naming the port, builds the selected port
with defaults, then each eligible non-default variant separately; `universal` is
excluded. It multiplies work across environments. More than 12 builds requires
confirmation, or `--yes` without a terminal. GitHub checks do not accept variant
selection because its workflow controls variants. A port checked only with
variants is named by the build that ran, as `jq +docs`, in status, submit's
plan and the description, and `--accept` takes that name. A variant build is evidence
for its files: a later check of the same files with other variants, or none,
adds its builds beside it rather than replacing it, and the pull request's table
shows each (`dockhand usage publishing`).

## Choose build environments explicitly

`--build` selects a configured profile; `--on` selects explicit environments.
They are mutually exclusive.

```sh
dockhand check --build host
dockhand check --build minmax --plan
dockhand check --on tart:sequoia --on tart:tahoe
dockhand check --on github
dockhand check --on local
```

`--build host` builds on this Mac's macOS alone, the quick check;
`build.default = "host"` makes it the default.

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
the fallback default instead of `minmax`. `build.unattended` selects checks on
unattended publication paths such as `bump` and `serve`; otherwise its fallback
is the same `minmax` when Tart is available, or the ordinary default. Use
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

Tart requires Apple silicon. Dockhand finds `tart` on PATH, then in
`/opt/local/bin`; where neither has it, a check that needs it says so,
`build profile host requires Tart, which isn't on PATH or in /opt/local/bin`.
Each execution clones a prepared image and runs the shared build program inside
it. Ordinary checks remove the clone afterward; the prepared images remain
available for later checks. A cancel stops each clone the check made and
deletes it once its VM has gone, also for a check whose dockhand was killed;
until then the cancel stays unfinished and `status` says which clone. A clone
an attempt kept for `dockhand shell` stays, and the canceled check says so.

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
can wait for a slot without having failed. Over SSH with nobody at the screen,
the login Keychain is locked and macOS refuses to start a VM; the check says so
once, with how to unlock it, and makes no further attempt. `dockhand setup`
warns of it on its Tart line.

### What a check installs

check's plan estimates what each environment installs before it builds,
`Installs    about 14 dependencies to install, 3 with no archive on
packages.macports.org for this release, by the port index; the clone decides`.
Those with no archive build from source, which can take a while; it's a hint
for choosing `-d`, and the clone decides. `check --plan` looks the archives up;
a check says them as it starts. The archives a guest is given are said once,
`giving the guest 25 archives earlier guests installed and 2 kept of their
targets' builds`, each by name under `-v`. Kept archives whose files are
missing, as from a database copy without its archives directory, are said once
with their count and ports, `check-42: 3 saved archives for jq, oniguruma,
and libharbor are missing. Removing the missing archive records; the build
environment will use MacPorts archives or build from source`.

### GitHub Actions

`--on github` needs a login, a Git remote that pushes to your fork, and Actions
enabled there. It pushes a branch of the check's own,
`dockhand-check/<the commit's first 12 characters>-<the check's ID>`, and runs
MacPorts' own workflow. Every run on that branch is the check's, so two checks
of one commit never share a run, and a cancel of one stops only its own.
Dockhand reads the results and keeps logs locally, then removes the temporary
branch when it is done with it; `dockhand clean` removes one a canceled check
left. This has external effects even though it opens no PR.

A cancel of a GitHub check cancels every run on its branch and is finished
once each has completed and every push and rerun of the check has a run of
its own listed. A push GitHub accepted but hasn't listed a run for keeps the
cancel unfinished, however long ago it was, since its run can still start:
`status` keeps its line, and each `cancel`, and serve as it passes, looks
again. A push git reported rejected, or one dockhand refused before sending,
needs no run. A rerun GitHub refused is not yet read as having started
nothing, since GitHub doesn't say what its refusals mean.

Each macOS release in the workflow's matrix is its own environment, with its own
column, and one run builds them all. `--on github` asks for every release;
`--on github:15` is refused, so check one release with Tart or `local`. A
release whose job no runner took is left unchecked, and submit says so with
GitHub's reason; check that release with Tart or `local`, or try again later.
The workflow builds only the ports a commit changes, so the plan says which
`--also` or baseline targets it won't build. The pull request's Tested on gives
each runner's macOS version, architecture, Xcode, and MacPorts as MacPorts
wrote them in the runner's log, `macOS 15.8.2 arm64 · Xcode 16.4 · MacPorts
2.12.7 (macos-15)`; a runner that failed before writing them gives its release
alone.

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
`dockhand cancel` of the stopped one. Dockhand starts the guest program through
a small process of its own, `dockhand __starter`, which shows as the build's
parent in `ps`: it records itself in the check's directory and leads the
build's process group. A cancel, also of a check whose dockhand was killed,
signals that process, which stops the whole group and kills what remains after
30 seconds; the cancel finishes once the group is gone, then puts the prefix
back, or says what is left and that the next check in it, or
`dockhand undo check-N`, puts it back. A root build isn't stopped this way yet:
its cancel stays unfinished and names the command to stop it by hand, `sudo
kill -TERM -<group>`, then `dockhand cancel check-N` again. Restoring active versions leaves
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

The script's shell runs in a session of its own, under `dockhand __starter`,
which records itself in the check's log directory. A cancel, also of a check
whose dockhand was killed, interrupts the shell's whole process group, kills
what remains after the grace, and finishes once the group is gone. What the
script started on another machine, or moved out of its process group, is the
script's own to stop.

## Run in the foreground or queue it

Without `serve`, an ordinary check runs here. Ctrl-C stops that work, preserving
completed results, and so does closing the terminal or losing the SSH session
it runs in: the hangup stops the check as Ctrl-C does, stopping a Tart VM and
deleting its clone, even though nothing can show its output any more. With
`serve` leading, it runs the check and this command follows; Ctrl-C then stops
following, not the service's check.

```sh
dockhand check -d
dockhand wait
dockhand status
```

`-d` queues and returns. If no service is leading, Dockhand starts a background
`serve --drain` worker, whose log is `drain.log` beside the database. `wait`
follows the latest check of the current branch. The output also names the check,
such as `check-12`, for use from anywhere.

`--for 9m` on `check` or `wait` follows for at most nine minutes and then exits
4 if the check is still queued or running, saying what it is doing; the check
continues, with serve or a background worker, never in the command's own
process, so `wait --for` starts a worker where `wait` alone would have run the
check here. `check --baseline --for 9m` follows a baseline the same way. `dockhand wait check-42 --for 9m` follows it further.

A batch's checks, serve's, and a foreground check count against one capacity
for each provider: Tart's is as many checks as the Mac runs VMs, GitHub's and a
command provider's are `providers.github.capacity` and
`providers.command.capacity`, and a prefix takes one. A check that waits for a
slot says once what holds it, naming each check using one and its process,
`check-617 waits for a slot on command: providers.command.capacity is 1, and
check-616 (a foreground dockhand, pid 4242, v0.8.0, for 2 hours) is using it`,
or `waiting for /opt/local, held by check-786` for a prefix another check
builds in. `logs` says when a check was queued, how long it waited for its
slot, and when it finished,
`Queued 2026-10-31 14:02:11, waited 3m 31s for its slot, finished 2026-10-31
14:20:40.`, and `status`'s queue says each started check's wait.
`dockhand console` keeps the branches current as serve works, with the
journal's newest events under them; it replaces `status --watch`.

One branch runs one check at a time. A new request follows an active check only
if it covers the requested files, targets, environments, and policy. Otherwise
it refuses, as it does where a tag or branch a port fetches moved since the
active check was planned. `--replace` stops the active request, keeps completed
results, and queues the new one. `dockhand cancel` of a GitHub check whose
driver stopped cancels its workflow run on your fork too, and the check stays
unfinished, `canceling` in `status`, until GitHub reports the run completed. Read `dockhand usage recovery` before choosing between wait,
retry, replacement, and cancellation.

## Read results and reuse

| Result | What to do |
| --- | --- |
| `passed` | Inspect test policy, notices, and environment coverage before submitting |
| `failed` | Read the named port's failed phase and log |
| `blocked` | Fix the changed prerequisite that prevented this target building |
| `unmet` | Provide the requested environment capability, such as Xcode, or choose a suitable environment explicitly |
| Not run or interrupted | Determine whether work is still queued, resumable, excluded by plan, or needs another check; a result that no longer counts says why |

```sh
dockhand logs check-12
dockhand logs check-12 jq
dockhand logs check-12 jq --phase install
dockhand artifacts
```

Use the actual check and port names. Logs identify the build when several
environments exist; `--full` includes dependency installation, and `--follow`
follows supported live logs. `artifacts` lists kept archives of the branch's
latest check, or of a check it names, as `artifacts check-12`, or explains why
one is missing. A build that links another port's
library the Portfile doesn't declare is a notice, holding nothing, that names
the line to write: `omnidiff links libiconv.2.dylib from libiconv, which it
doesn't declare in depends_lib. Declare it with depends_lib-append
port:libiconv; dockhand tidy can write this line`. For supported declarations,
`dockhand tidy` writes a separate commit and carries over the check's results:
the build already had that library active. Subport and variant declarations
must be written in their appropriate blocks; tidy explains when it cannot
write the line.

Dockhand also reports a dependency declared in `depends_lib` whose libraries
none of the builds links: `declares libiconv in depends_lib, but none of its
builds links that port's libraries`. Consider
`depends_build` for build tools or headers, and `depends_run` for programs or
libraries loaded at runtime. Dependencies a PortGroup adds, ports installing
no library, and builds installing no Mach-O file are not reported. When a
declared port's pkg-config brings in an undeclared library, the finding says so: `through gtk4's
pkg-config Libs`, and `-Wl,-dead_strip_dylibs` as the other way out.

Tart results can be reused when recorded input content and environment identity
still match, including dependencies and fetched Git commits. Where an
environment's identity can't be read, as when Tart doesn't answer, its results
are taken as they were and status says so, with the cause; serve and `ship
--yes` hold a submission resting on them (`identity-unread`) until it reads
again, and your own submit says it and goes on. `check` and `wait` say each
beside the results, `check --json` and `wait --json` list them under
`identities_unread`, and `status --json` gives the latest check's under its
`identities_unread`. A Portfile whose only change declares a port the recorded build had active is
the same input, so its results carry over, as an edit of metadata no build
reads does. A result is keyed
by the PortGroups MacPorts opened each port with, not the whole of
`_resources`: a change to a PortGroup the port didn't load leaves it standing,
and a change to one it did voids it. A dependency the check built from source
counts its own PortGroups; one installed from an archive doesn't. A kept
dependency archive is offered only while the PortGroups its build loaded stand,
and a check says once when it isn't: `check-12: the saved archive for cargo was
built before the github 1.0 PortGroup changed, so the build environment will
use a MacPorts archive or build it from source`. `--fresh` requests a rebuild.
A rebase can preserve valid evidence when those inputs did not change. Results
from separate checks can jointly cover the same branch files; a narrow rerun
does not erase other still-valid results. Moved tags or changed inputs can make
evidence stale even when a version string did not change. A check whose Git
source couldn't be read stands for itself alone, never for a later request. A
result built before a GitHub tag moved, or on a release an earlier check built
from another commit of the same ref, is built again; on that release, results
of other ports still count, and the release is still required.

Where an earlier check's result no longer counts, `status` shows the port as
not run there and submit holds, both saying why in the same words:
`built by check-1 from v4 at 1a2b3c4, which now names 5d6e7f8, so it doesn't
count; dockhand check builds it again`. A port built against a dependency whose
source moved names that dependency: `built by check-1 against libharbor from v4
at 1a2b3c4, which has moved`. Other reasons are a source that couldn't be read
when the check was planned (`stands for that check alone`) or can't be read now,
a Git build that didn't record the commit it fetched, a port fetched with Git
then and another way now, a dependency whose own result doesn't count, and a
blocked port whose blocker's result no longer stands. Only a port no check built
says `planned by check-4, but the check stopped before building it`.

A change that builds nothing differently is carried over: a Portfile edit to
`maintainers`, `categories`, `description`, `long_description`, `homepage`, or
`livecheck`, or a directory move, reuses the earlier check's builds without
starting a guest, running lint again on this Mac where the Portfile changed. The
check's result, `status`, and the pull request's Tested on say so:
`check-767 reused check-766's builds (commit 3f2a1c9): since then only
omnidiff's maintainers changed, which builds nothing differently, and lint ran
again on this Portfile`. A lint error builds the target again.

By default, a failed Tart check keeps its clone, stopped, one for each branch
and release, for `dockhand shell`: the branch's next check or `clean` removes
it, and serve's daily cleanup removes one older than seven days. `[providers.tart]
keep_failed = false` turns that off. To retain a passed build too, request
`--keep` before running it:

```sh
dockhand check --keep
dockhand shell check-12 -- otool -L /opt/local/bin/PROGRAM
```

`dockhand shell check-12` while check-12 is still running on Tart enters its
VM; what the check builds after the shell opened isn't evidence.

Replace the identifiers with the check and installed program. `shell --test` can
try edits to the test phase in a retained clone, but that experiment is not
recorded check evidence. See `dockhand usage recovery` for baselines and
debugging, and `dockhand usage publishing` for the evidence required to submit.
