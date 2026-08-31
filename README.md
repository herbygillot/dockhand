<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/logo-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="images/logo-light.png">
    <img alt="dockhand logo" src="images/logo-light.png" width="480">
  </picture>
</p>

# dockhand

**From upstream release to submitted port.**

A port maintenance utility for [MacPorts](https://www.macports.org).

> [!WARNING]
> **This is a work in progress and is changing constantly.** Packages get
> renamed, commands get added, and signatures move between commits. There
> is no release, no stability promise, and no migration path. Try it, take
> ideas from it, but do not build on it yet.

## What it is

Keeping a MacPorts port up to date is mechanical and tedious: read the
new version, fetch the distfile, update three checksums, reset the
revision, regenerate whatever a PortGroup vendored, and hope nothing else
moved. `dockhand` does that work, and by default it makes the change
rather than suggesting one — so the interesting part is how it avoids
getting it wrong.

**It never parses a Portfile to decide what it means.** A Portfile is a
Tcl program, so the only thing that can say what it evaluates to is
MacPorts. `dockhand` keeps a `port-tclsh` session open and asks. Where
the answer would be a reimplementation of MacPorts, it drives MacPorts
instead: its curl for fetching, its `livecheck` for upstream checks, its
version comparison for ordering. The same principle covers git — the
real `git` is driven through its plumbing, never reimplemented.

**It edits as text, never as a syntax tree.** Changes are byte-span
replacements, so comments, alignment, and every line nobody asked about
survive untouched. The diff is what a maintainer would have written by
hand.

**It predicts, then checks.** Before anything real happens, `dockhand`
copies the port to a temporary directory, applies the edits there, and
re-evaluates. That gives an exact prediction of what will change, and
the realized change must equal the prediction or nothing happens. A
change does precisely what it said or it does nothing.

**It works in branches, not in your files.** In a git checkout of the
ports tree, a bump becomes a commit on a fresh `dockhand/…` branch,
created directly in git's object database — your checked-out files,
your HEAD, and whatever you were in the middle of are never touched.
The branch is then built in a clean virtual machine in the background
while you do something else.

When it can't be sure, it declines and says why, rather than guessing.

## Using it

`dockhand` needs a MacPorts installation and a ports tree, and finds both
on its own. The installation comes from `port-tclsh` on your `PATH`. The
tree is whichever one you are standing in — run it from anywhere inside a
checkout, and there is nothing to configure:

```bash
cd ~/Source/macports-ports/devel/tokei
dockhand classify .
```

The branch workflow needs the tree to be a git checkout. In a tree
without git — the rsync tree MacPorts maintains, for instance — the
read-only commands work as ever, and writing needs `--in-place`.

To work on a tree from outside it, name it — by flag, or once in your
shell:

```bash
export DOCKHAND_TREE=~/Source/macports-ports
```

`--tree` wins over `$DOCKHAND_TREE`, which wins over the working
directory. `--prefix` and `$DOCKHAND_PREFIX` do the same for the
installation.

### What can this machine do?

```bash
dockhand doctor
```

Every capability depends on a tool, and a missing one is a fact about
your machine rather than a failure — so it is reported up front instead
of forty minutes into a batch.

```
  port-tclsh   /opt/local/bin/port-tclsh  (2.12.6)
  git          /opt/local/bin/git  (2.55.0)
  tart         /opt/local/bin/tart
  cargo2port   /opt/local/bin/cargo2port
capabilities:
  evaluation               available
  branches and worktrees   available
  VM verification          available (Monterey, Sonoma, Sequoia)
  Rust vendored blocks     available
```

### Which ports can be worked on automatically?

```bash
dockhand classify lang
```

Ports declare their version in many ways — a literal `version` line, a
`github.setup` argument, a PortGroup's own convention. `classify`
surveys a port, a category, or the whole tree (`--all`) and reports which
ones `dockhand` can locate a version in. Add `--declines` to list the
ones it can't, with the reason.

```
435 ports classified
  located          396  (91.0%)
  not literal       39  (9.0%)
located by style:
  version          270
  github.setup     104
  go_toolchain.setup     8
```

## The workflow

### Bump a port

```bash
dockhand bump jq                    # to the newest upstream release
dockhand bump --to 1.8.2 jq         # to a version you name
```

Ports are named the way `port` names them — a port, a subport, a
directory, or `.` in a portdir.

With no `--to`, the newest release is resolved two ways and the answers
compared, which is what the verdict in `latest: 1.8.2 (agreement)`
reports: the port's own `livecheck`, and the tags on its upstream forge.
A disagreement is worth knowing about — it usually means a `livecheck`
regex has quietly rotted while releases kept happening.

Everything is computed before anything is realized, fetching included —
the checksums are of bytes actually downloaded, and a port with a
vendored `cargo.crates` block has it regenerated from the `Cargo.lock`
inside that very distfile. The summary prints first, then the change
becomes a branch:

```
plan: bump …/sysutils/jq, 4 edits
  version:         1.8.0 -> 1.8.2
  checksum rmd160: cb557191b2698d3f05b0a0166170d295dd085ea0 -> …
  checksum sha256: 71b8d6e8f5fe81f6c6d0d110e3892251f6ce76ed… -> …
  checksum size:   1959950 -> 2026798
predicted delta:
  jq: checksums …; distfiles …; version …
branch: dockhand/jq-1.8.2 (a6fa8619f5ea)
your checkout is untouched — `git checkout dockhand/jq-1.8.2` to add changes
verify: submitted jq (job dockhand-worker-6f2a1b); `dockhand status` follows it
```

The commit is already in the project's format (`jq: update to 1.8.2`),
its parent is your primary branch, and a build of the changed port has
been submitted to a clean virtual machine. `dockhand` exits; the VM
carries on without it.

Two sibling intents work the same way: `refresh-checksums` repairs
recorded checksums at an unchanged version (and warns loudly, every
time, that checksums moving at the same version deserve an explanation
before the change goes anywhere public), and `bump-revision`
(`revbump`) increments the revision, with `--reason` required — the
reason becomes the commit message, because why users must rebuild is
exactly what the log should say.

### Follow the verification

```bash
dockhand status
```

```
dockhand/jq-1.8.2                verifying (27s)
```

and later:

```
dockhand/jq-1.8.2                passed
```

`status` polls running builds, records verdicts, and frees the VM on a
pass. A failed build keeps its VM alive as the debugging environment,
and says so.

### Add your own commits

The branch is an ordinary git branch. Check it out, make the edits
dockhand couldn't, commit, and re-verify — the whole branch, whoever
wrote it:

```bash
git checkout dockhand/jq-1.8.2
# edit, commit …
dockhand verify dockhand/jq-1.8.2
```

`status` notices when a branch has moved past what was verified —
including the case where only the commit message changed, which it
reports as verified content rather than asking for a rebuild. A
`verify` of a moved branch cancels the stale build before starting the
real one.

### Open the pull request

```bash
dockhand promote jq
```

`promote` refuses an unverified branch. Otherwise it pushes the branch
to your fork — found by matching your `gh` login against your remotes —
and opens the PR against the upstream repository, titled with the
commit's subject and carrying a plain statement of what was verified.
`--closes 12345` links a Trac ticket; `--no-pr` stops after the push.

### Sweep up

```bash
dockhand clean       # remove branches whose PRs merged
dockhand discard jq  # remove one branch, verified or not
```

`clean` asks GitHub, not git: the project's merge styles rewrite
commits as they land, so a merged branch never looks merged to
`git branch --merged`. The PR's own state decides, double-checked by
comparing the touched files' bytes against your primary branch, and
everything kept says why — an open PR, a rejection, a branch never
promoted. `discard` deletes one branch and releases everything it
holds, including a failed build's kept VM.

### Look before you leap

Three output modes stop short of a branch:

```bash
dockhand bump --diff --to 1.8.2 jq   # print the patch, as git renders it
dockhand bump --plan --to 1.8.2 jq   # print the computation, as JSON
dockhand bump --in-place jq          # edit the working tree, no branch, no commit
```

`--diff` prints exactly the patch the branch would carry — `git apply`
accepts it — and pages through your own pager the way `git diff` does.
`--in-place` is for folding dockhand's edit into a workflow you are
already running yourself; the prediction check still guards it.

### Re-derive at the current version

A port already at the newest release declines — there is nothing to bump
to. `--force` proceeds anyway, which is how you catch an upstream that
re-rolled a release at the same version and the same URL:

```bash
dockhand bump --force jq
```

The version is not rewritten to itself and the revision is left alone;
what gets re-derived is everything downstream — the distfile is fetched
again and its checksums compared, and a vendored block is regenerated
from the lockfile inside it. If nothing moved there are no edits and no
branch, which is the common and correct outcome.

## Verification environments

Builds run in disposable [tart](https://tart.run) virtual machines,
each cloned fresh from a base image that has macOS, the command line
tools, MacPorts, and nothing else — no Homebrew, no leftovers from
earlier builds. Build one base per macOS release you care about:

```bash
dockhand provision tart --macos sequoia
```

Provisioning proves what it built — no foreign package manager, a
working compiler, MacPorts answering — and keeps a golden copy that
never runs, so a base that drifts is restored by a free clone
(`--restore`) rather than a rebuild. `--recheck` re-runs the proofs on
demand. `bump --on sonoma` and `verify --on sonoma` choose the release
to build on.

## Exit codes

Failures say whose problem it is:

| | |
|---|---|
| `0` | success |
| `1` | the operation failed |
| `2` | bad flag, unknown command, invalid arguments |
| `3` | the machine — MacPorts missing, no VM available |
| `4` | the ports tree — not a tree, port not found |
| `5` | an intent declined: nothing to do, or not safely doable |
| `6` | verification ran, and the port does not build |

One case splits progress from contract: if the branch was created but
its verification could not start — no base images, every VM slot taken —
the exit is `3` while the branch stands, the way a failed `git push`
never deletes the commit. The message names the follow-up, and
`--no-verify` narrows the contract to just the branch.

## Building

Go 1.27 and nothing else. Dependencies are vendored, so no network is
needed to build:

```bash
make build
```

```bash
make test     # go test -race ./...
make check    # gofmt, go vet, and the linters CI runs
```

Tests gate themselves on what the machine has, so `go test ./...` passes
on a machine with only Go installed — anything needing `port-tclsh`,
`tclsh`, `git`, or `cargo2port` skips instead of failing. To demand them
where they are supposed to exist:

```bash
DOCKHAND_TEST_REQUIRE=1 make test        # every tool
DOCKHAND_TEST_REQUIRE=tclsh,git make test   # only these
```

## Design

`docs/` holds the reasoning: the conceptual one-pager, the intent
catalogue, the reading and verification models, field evidence, and the
decision log. They are records of *why*, not commitments. When an
assumption expires — a platform floor moves, a PortGroup changes
behavior, `port bump` improves — the useful question is whether the
original reason still holds, and that is only answerable if the reason
was written down beside the decision.
