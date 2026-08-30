<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/logo-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="images/logo-light.png">
    <img alt="dockhand logo" src="images/logo-light.png" width="480">
  </picture>
</p>

# dockhand

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
moved. `dockhand` does that work — but the interesting part is how it
avoids getting it wrong.

**It never parses a Portfile to decide what it means.** A Portfile is a
Tcl program, so the only thing that can say what it evaluates to is
MacPorts. `dockhand` keeps a `port-tclsh` session open and asks. Where
the answer would be a reimplementation of MacPorts, it drives MacPorts
instead: its curl for fetching, its `livecheck` for upstream checks, its
version comparison for ordering.

**It edits as text, never as a syntax tree.** Changes are byte-span
replacements, so comments, alignment, and every line nobody asked about
survive untouched. The diff is what a maintainer would have written by
hand.

**It predicts, then checks.** Before touching anything, `dockhand` copies
the port to a temporary directory, applies the edits there, and
re-evaluates. That gives an exact prediction of what will change. Making
the change for real re-evaluates again and demands the observed change
equal the prediction — and if it doesn't, the Portfile is restored. A
change does precisely what it said or it does nothing.

When it can't be sure, it declines and says why, rather than guessing.

## Using it

`dockhand` needs a MacPorts installation and a ports tree, and finds both
on its own. The installation comes from `port-tclsh` on your `PATH`. The
tree is whichever one you are standing in — run it from anywhere inside a
checkout, or inside the rsync tree MacPorts already maintains, and there
is nothing to configure:

```bash
cd ~/Source/macports-ports/devel/tokei
dockhand classify .
```

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
  cargo2port   /opt/local/bin/cargo2port
capabilities:
  evaluation               available
  branches and worktrees   available
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

`bump` writes the change. Before it does, it prints what it is about to
do:

```
plan: bump …/www/geckodriver (subport geckodriver), 5 edits
  version:         0.30.0 -> 0.37.1
  checksum rmd160: 14b9c9ce7bd5e646401f2e230b558c381b5836ab -> bb45824855…
  checksum sha256: b284847a730f2bb95c5b9145972a7686f8c0462b26… -> 229cafca8a…
  checksum size:   83751 -> 120590
  regenerate cargo.crates: …237 crates…
predicted delta:
  geckodriver: checksums …; distfiles …; version …
```

*(hashes, the regenerated block, and the delta's field values are elided
here — the real output prints them in full)*

Everything is computed before anything is written, fetching included — so
the checksums are of bytes actually downloaded, and a port with a
vendored `cargo.crates` block has it regenerated from the `Cargo.lock`
inside that very distfile.

Writing is not the same as trusting. The edit is applied, the port
re-evaluated, and the result compared against the prediction; anything
other than an exact match restores the original file. A bump does
precisely what it said or it does nothing.

### Look before you leap

`--plan` computes everything and stops, emitting the plan as JSON on
stdout so you can read the summary and keep the plan:

```bash
dockhand bump --plan --to 0.37.1 geckodriver > plan.json
dockhand apply plan.json
```

`apply` runs the same checks, plus one more: it refuses if the Portfile
changed since the plan was made.

### Exit codes

Failures say whose problem it is:

| | |
|---|---|
| `0` | success |
| `1` | the operation failed |
| `2` | bad flag, unknown command, invalid arguments |
| `3` | the machine — MacPorts missing, `tclsh` broken |
| `4` | the ports tree — not a tree, port not found |
| `5` | an intent declined to produce a plan |

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
