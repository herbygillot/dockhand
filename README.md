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

`dockhand` automates the mechanical part of maintaining a port: it bumps
a port to a new upstream release, build-tests the change in a clean
virtual machine, and opens the pull request — each step on its own, or
all three from a single command. When it can't be sure of something, it
declines and says why, rather than guessing.

## What it needs

A MacPorts installation, `git`, and `gh` (the GitHub CLI). With
[tart](https://tart.run) installed, `dockhand` can also build-test ports
in disposable VMs; without it, everything else still works and changes
are simply promoted unverified.

Run it from anywhere inside a checkout of the ports tree — there is
nothing to configure. To work on a tree from outside it, name it with
`--tree` or `$DOCKHAND_TREE`; `--prefix` and `$DOCKHAND_PREFIX` do the
same for the MacPorts installation.

## The workflow

### Bump a port

```bash
dockhand bump jq                    # to the newest upstream release
dockhand bump --to 1.8.2 jq         # to a version you name
```

Ports are named the way `port` names them — a port, a subport, a
directory, or `.` in a portdir. Everything is computed before anything
is realized: the distfile is fetched, checksums are of bytes actually
downloaded, and a vendored `cargo.crates` block is regenerated from the
`Cargo.lock` inside that very distfile. The summary prints first, then
the change becomes a branch:

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
verify: submitted jq on Tahoe (job dockhand-worker-6f2a1b); `dockhand status` follows it
```

The commit is already in the project's format (`jq: update to 1.8.2`),
its parent is your primary branch, and your checked-out files are never
touched. `dockhand` exits; the VM carries on without it — or stay
attached with `--trace`, which streams the build log until the verdict.

Two sibling intents work the same way: `refresh-checksums` repairs
recorded checksums at an unchanged version, and `bump-revision`
(`revbump`) increments the revision, with `--reason` required — the
reason becomes the commit message.

`bump-revision --for <branch>` is the same verb asked a different
question: it accepts a revision-bump proposal a verification measured,
revbumping the dependents the change put forward as one more commit.
`--exclude` leaves some of them out, and `--force-withheld` seats a
member the cohort had withheld — built last, with the sibling it
conflicts with deactivated first.

### Follow the verification

```bash
dockhand status
```

```
dockhand/jq-1.8.2                verifying (27s) (Tahoe)
```

and later:

```
dockhand/jq-1.8.2                passed (Tahoe)
```

`status` polls running builds, records verdicts, and frees the VM on a
pass. A failed build keeps its VM alive as the debugging environment —
`dockhand log jq` prints its build log (`--trace` follows it live), and
`dockhand shell jq` opens a shell inside it. `--keep-env` on `verify` or
`bump` keeps a passing build's VM too, for looking inside a green one.
`status` changes nothing you or anybody else can see: where work is
waiting — a run queued for a slot, a merged pull request whose branch
is still here — it says so and names `dockhand cycle`, which does it.

### Add your own commits

The branch is an ordinary git branch. Check it out, make the edits
dockhand couldn't, commit, and re-verify — the whole branch, whoever
wrote it:

```bash
git checkout dockhand/jq-1.8.2
# edit, commit …
dockhand verify dockhand/jq-1.8.2
```

`status` notices when a branch has moved past what was verified, and a
`verify` of a moved branch cancels the stale build before starting the
real one. `verify --on <release>` picks the macOS release to build on;
`--on all` runs the whole provisioned matrix.

### Open the pull request

```bash
dockhand promote jq
```

`promote` refuses an unverified branch. Otherwise it pushes the branch
to your fork — found by matching your `gh` login against your remotes —
and opens the PR against the upstream repository. The title is the
commit's subject, and the body is the project's own PR template with
what was actually verified checked off. Before opening anything it
searches upstream's open PRs for the same change, and refuses to file a
duplicate. `--closes 12345` links a Trac ticket; `--no-pr` stops after
the push.

### Sweep up

```bash
dockhand cycle       # retire branches whose PRs merged; start deferred runs
dockhand discard jq  # remove one branch, verified or not
```

A branch whose PR merged is done: `status` reports it and names
`cycle`, and `cycle` deletes it locally and on your fork (`--keep-merged`
withholds that), and everything kept says why — an open PR, a
rejection, a branch never promoted. `cycle` also starts the runs that
were waiting for a slot; `--superseded` removes the branches a newer
sibling replaced, and `--reclaim-orphans` frees the VMs no note claims.
`discard` deletes one branch and releases everything it holds, including
a failed build's kept VM.

### Look before you leap

Three output modes stop short of a branch:

```bash
dockhand bump --diff --to 1.8.2 jq   # print the patch, as git renders it
dockhand bump --plan --to 1.8.2 jq   # print the computation, as JSON
dockhand bump --in-place jq          # edit the working tree, no branch, no commit
```

A port already at the newest release declines — `bump --recheck`
proceeds anyway, re-deriving checksums and vendored blocks at the same
version, which is how you catch an upstream that re-rolled a release.
It verifies from source, because an archive built for a version that
did not move says nothing about the distfile just fetched.

## What can this machine do?

```bash
dockhand doctor
```

Every capability depends on a tool, and a missing one is a fact about
your machine rather than a failure — so it is reported up front instead
of forty minutes into a batch.

```
  port-tclsh   /opt/local/bin/port-tclsh  (2.12.6)
  git          /opt/local/bin/git  (2.55.0)
  gh           /opt/local/bin/gh  (gh version 2.98.0)
  tart         /opt/local/bin/tart
  cargo2port   /opt/local/bin/cargo2port
capabilities:
  evaluation               available
  branch workflow          available
  GitHub integration       available
  VM verification          available (Monterey, Sonoma, Sequoia, Tahoe)
  Rust vendored blocks     available
```

## Which ports can be worked on automatically?

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

## How verification works

Builds run in disposable [tart](https://tart.run) virtual machines,
each cloned fresh from a base image that has macOS, the command line
tools, MacPorts, and nothing else — no Homebrew, no leftovers from
earlier builds. The port is installed exactly as a user would install
it, and `--test` on `bump` or `verify` also runs the port's own test
suite. Build one base per macOS release you care about:

```bash
dockhand provision tart --macos sequoia
```

Provisioning proves what it built — no foreign package manager, a
working compiler, MacPorts answering — and keeps a golden copy that
never runs, so a base that drifts is restored by a free clone
(`--restore`) rather than a rebuild. `--recheck` re-runs the proofs on
demand. With `--xcode <dir>` pointing at downloaded Xcode `.xip`
archives, the image also gets the newest full Xcode its release can
run — which is what lets ports that need `xcodebuild` verify too.

Verdicts are recorded per commit and per macOS release, so one branch
can carry `passed (Sonoma)` alongside `unsupported (Monterey)` — and
`promote` states exactly that set in the PR.

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

## License

[MIT](LICENSE)
