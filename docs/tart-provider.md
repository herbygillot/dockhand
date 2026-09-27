# The Tart provider

`check --on tart` builds in a fresh clone of one of dockhand's Tart images,
one clone for each macOS release and attempt, deleted afterwards (Design v3
§7). A clone left by a process that died is deleted by the check's next
attempt, or else by `dockhand clean` or serve's daily cleanup, once no
process runs its check. It follows MacPorts CI's order (decisions 11 and 22). For each target,
in dependency order:

1. everything installed is deactivated;
2. the target is linted;
3. its dependencies are installed and activated, from MacPorts' binary
   archives where they exist;
4. it is fetched, checksummed, and installed;
5. its declared tests run. As in CI they are advisory unless the check
   says `--tests required`.

A target whose changed dependency failed is recorded as blocked, not built
against an old build of the dependency. Each target's result is recorded
as it finishes, so a guest lost midway keeps what it finished, and the
next attempt builds only the rest.

## Choosing releases

| `--on` | Builds on |
|---|---|
| `tart` | this Mac's macOS |
| `tart:sonoma,tahoe`, or `tart:14,26` | those releases, each a guest of its own |
| `tart:all` | every release dockhand has an image for |
| `tahoe` | a bare release name means Tart |

Several releases must all pass. `[check] on = ["tart"]` in
`~/.dockhand/config.toml` makes Tart the default. Without it, a check uses
your own script if `[providers.command]` is set up, and Tart on this Mac's
release otherwise.

## Images

Each release needs `dockhand-base-<release>`, such as `dockhand-base-tahoe`,
in dockhand's own Tart home, `~/.dockhand/tart` (or `$DOCKHAND_TART_HOME`).
Each image holds macOS, the Command Line Tools of the release's pinned
generation (from the [facts table](../tools/facts/README.md)), and
MacPorts. A check naming a release without an image stops before it starts,
naming the image it needs.

`dockhand providers setup tart` makes this Mac's release's image, and
`dockhand providers setup tart sonoma` makes Sonoma's. It starts from Cirrus
Labs' vanilla macOS image, downloaded the first time, and takes up to 60 GB
of disk. A golden copy is kept beside each image, sharing its blocks, and a
lost image is restored from it. An image that exists is checked in a
disposable clone instead and left as it is. `--check` only checks, and
`--rebuild` makes a replacement, keeping the old image until the new one
passes. An image `v2-final`'s `setup` made in `~/.tart` is copied in, when
it still passes, rather than made again.

Golden Gate's images, macOS 27, have ASIF disks where earlier releases'
are raw. They need Tart 2.39.0 or newer, the first to list its VMs while
one with an ASIF disk runs (openai/tart#1344); setup refuses them on an
older Tart before the clone ever runs. An ASIF disk is grown by Tart
itself, which moves the guest's recovery partition to the new end, so
nothing on the host is edited. It is given 125 GB rather than 100, since
the recovery partition stays and macOS 27 keeps more of the disk: a 125 GB
Golden Gate guest has about 79 GB free. ASIF is sparse, so the extra size
takes no host disk until it is written.

`dockhand providers` shows which releases have images, with Xcode or not,
and whether the other providers are ready. `init` shows the same.

## Xcode

Xcode is an add-on. A release's checks need only its base image, with the
Command Line Tools.
`dockhand providers setup tart tahoe --xcode <Xcode .xip, or a folder of them>`
makes Tahoe's Xcode image, `dockhand-xcode-tahoe`, with the newest Xcode
Tahoe runs, in up to 65 GB of disk. Xcode comes from Apple, as a `.xip`
from developer.apple.com.

**With its Xcode image, a release builds there, every port with Xcode.**
MacPorts' builders have Xcode too, and a port that doesn't ask for it
still builds with the Command Line Tools. The plan's Provider line says
which it is: "tart macOS 26 (Tahoe) arm64 with Xcode".

**A release is planned with the tools it builds with.** It's modelled
from the facts table's row for them, this Mac's own release too, so a
Portfile that chooses by Xcode's version plans as it builds. When the
image's Xcode or tools differ from that row, say after setup with a newer
`.xip`, the check reports the drift.

**Without it, a release builds with the Command Line Tools alone**, and
doesn't build what needs Xcode:

- a port that needs Xcode itself, when MacPorts, reading it for that
  release, says so (`use_xcode`): it asks for Xcode, or builds with
  `xcodebuild`;
- a port whose prerequisite needs it: a changed port the check builds
  before it, from source.

Such a port is **unmet**. `check --plan` says so before the check, with the
command that makes the Xcode image, and the result says "not built: needs
Xcode" (", through libharbor" when a prerequisite needs it). It is never
tried. Nothing failed, so the check needs attention rather than failing,
and a check with nothing it can build doesn't start. `submit` still needs
those ports checked, with Xcode, or on `--on github`, whose runners have
it.

Unchanged dependencies are installed from MacPorts' binary archives, which
need no Xcode. When one has no archive and needs Xcode, MacPorts itself
refuses to build it in the guest. The target then fails at install, with
MacPorts' reason.

## Settings

```toml
[providers.tart]
capacity = 1          # checks serve runs at once; 1 when unset
test_timeout = "45m"  # a target's tests; 30 minutes when unset
```

macOS runs two VMs at most, yours among them, so a check waits for a slot
when two are already running.

## What it reports

Every provider run, one attempt in one release, has a unique ID named for
its provider, `tart_7y62p4sigena6xlr`:

- the check's progress shows it ("attempt 1 of 3 on tart macOS 26 (Tahoe)
  arm64 with Xcode, run tart_7y62p4sigena6xlr");
- a pull request's Tested on names it, beside the macOS, Xcode, and tools
  the guest reported;
- `dockhand logs tart_7y62p4sigena6xlr` shows that run's evidence, and its
  VM clone's name.

Each target's log is copied into the check's log directory (`dockhand
logs`). When the guest's Command Line Tools, or its Xcode in an Xcode
image, differ from the facts table's row the plan was read with, the
check says so as a drift report, which never changes a result
(decision 10).
