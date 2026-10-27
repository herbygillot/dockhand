# The Tart provider

`check --on tart` builds in a fresh clone of one of dockhand's Tart images,
one clone for each macOS release and attempt, deleted afterwards (Design v3
§7). A clone left by a process that died is deleted by the check's next
attempt, or else by `dockhand clean` or serve's daily cleanup, once no
process runs its check. It follows MacPorts CI's order (decisions 11 and 22). For each target,
in dependency order:

1. everything installed is deactivated, and the target's earlier work is
   cleaned, as another variant's build of it leaves one;
2. the target is linted;
3. its dependencies are installed and activated, from MacPorts' binary
   archives where they exist;
4. it is fetched, checksummed, and installed from its source (`port -s`),
   never from a published archive, as CI's `mpbb install-port --source`
   builds it: an archive of the same version, revision, and variants is
   master's Portfile's build, not the branch's;
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

Several releases must all pass. Without `--on`, a check builds where a
build profile says: `--build`'s, else `build.default`'s, `host`, this Mac's
release, unless set. dockhand's own profiles all run on Tart: `host`,
`minmax`, each port's oldest and newest release you have an image of,
macOS 12 at the earliest, `macports-ci`, and `all`.

## Images

Each release needs `dockhand-base-<release>`, such as `dockhand-base-tahoe`,
in dockhand's own Tart home, `~/.dockhand/tart` (or `$DOCKHAND_TART_HOME`).
Each image holds macOS, the Command Line Tools of the release's pinned
generation (from the [facts table](../tools/facts/README.md)), and
MacPorts. A check naming a release without an image stops before it starts,
naming the image it needs.

`dockhand setup tart` makes this Mac's release's image, and `dockhand
setup tart sonoma` makes Sonoma's. It starts from Cirrus Labs' vanilla
macOS image, downloaded the first time, and takes up to 60 GB
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

`dockhand setup`'s Providers rows show which releases have images, with
Xcode or not, and whether the other providers are ready.

### What an image is made from

Setup pins the vanilla image it starts from by content. It asks the
registry, through the OCI distribution API, which digest the image's tag
names, before pulling the tag and again after. Tart documents pulling by
tag, not by digest, so the digest is read from the registry rather than
given to Tart. If the tag moved while it was pulled, setup stops and asks
to be run again. If the registry can't say, setup says the image's origin
is unknown and carries on.

Each image keeps every port's archive. It sets MacPorts' documented
`portimage_mode` to `directory_and_archive`, since on APFS MacPorts
otherwise deletes an archive once it has extracted it. A check identifies
the ports active as a target built by their archives.

With each image it makes, setup records that image's origin on this Mac,
in `~/.dockhand/tart-images/`: the vanilla image's digest, the version of
setup's own steps, and the MacPorts, Command Line Tools, and Xcode it
installed. A check reads it without starting the VM. The origin, with the
version of the guest program that verifies each port, is the release's
identity, which each provider run records as it begins.

A result stands for the image it was built in. Once an image is made
again, from a newer vanilla image or with other tools, what passed in the
old one no longer counts, and `status` and `submit` ask for the ports to
be checked again. Rebuilding from the same source with the same tools
keeps the identity, and so the results. An image with no recorded origin,
one made before dockhand recorded origins, keeps its results as before.

## Xcode

Xcode is an add-on. A release's checks need only its base image, with the
Command Line Tools.
`dockhand setup tart tahoe --xcode <Xcode .xip, or a folder of them>`
makes Tahoe's Xcode image, `dockhand-xcode-tahoe`, in up to 65 GB of disk.
Xcode comes from Apple, as a `.xip` from developer.apple.com.

**An Xcode image has the Xcode MacPorts' arm64 buildbot for the release
runs,** so a port that needs Xcode builds as MacPorts builds its packages.
The facts table records each builder's Xcode from its logs. `dockhand
config` lists each release's:

| Release | Xcode |
| --- | --- |
| Monterey | 14.0.1 |
| Ventura | 14.3.1 |
| Sonoma | 15.4 |
| Sequoia | 16.4 |
| Tahoe | 26.6 |
| Golden Gate | 27.0 |

Setup takes the archive of exactly that version, `Xcode_26.6.xip` or its
`_Apple_silicon` or `_Universal` form, never a newer one in its place,
nor a beta or a release candidate. The Xcode must still run on the
release. Setup takes an archive only once `pkgutil --check-signature`
says it is Apple's, since the guest's `xip --expand` doesn't check.
Without the archive, setup says which Xcode to download.

With [xcodes](https://github.com/XcodesOrg/xcodes) installed (`sudo port
install xcodes`), setup downloads the missing Xcode into the folder, and
finds xcodes' names, `Xcode-15.4.0+15F31d.xip`, as it finds Apple's:
- **At a terminal,** setup asks first. xcodes asks there for your Apple ID
  and two-factor code when it needs them, and keeps the password in your
  Keychain and its session; dockhand never sees them.
- **Without a terminal,** setup downloads with the sign-in xcodes kept.
  When xcodes fails, as it does at once when it has none, setup shows
  what xcodes said and how to sign in once at a terminal.

xcodes signs in through Apple's own sign-in, which Apple doesn't document
for other programs, so it can stop working when Apple changes it; then
download the archive from Apple by hand.

`providers.tart.xcode` names another Xcode for a release, such as the
16.2 MacPorts' GitHub CI pins on Sonoma:

```toml
[providers.tart.xcode]
sonoma = "16.2"
```

The buildbots' Xcode changes when their administrators upgrade it. The
facts table follows when it is regenerated (`tools/facts`), and a check
reports where a guest's Xcode differs from it.

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
test_timeout = "45m"  # a target's tests; 30 minutes when unset

[providers.tart.xcode]  # a release's Xcode image, by name or number
tahoe = "26.6"          # what MacPorts' arm64 buildbot runs when unset
```

macOS runs two VMs at most, yours among them, and that is the provider's
only limit: there is no capacity setting, and a configuration that still
sets `providers.tart.capacity` is refused by name. A check waits for a slot
when two are already running. A check of several releases builds two of
them at once. Their VMs start one at a time, each once the last is
running, so when your own VM holds one slot they take the other in turn.

## What it reports

Every provider run, one attempt in one release, has a unique ID named for
its provider, `tart_7y62p4sigena6xlr`:

- the check's progress shows it ("attempt 1 of 3 on tart macOS 26 (Tahoe)
  arm64 with Xcode, run tart_7y62p4sigena6xlr");
- a pull request's Tested on names it, beside the macOS, Xcode, tools, and
  MacPorts the guest reported. Where a port's results came from runs that
  found the release otherwise, such as an earlier check's with other
  tools, each report is its own line, naming its runs;
- `dockhand logs tart_7y62p4sigena6xlr` shows that run's evidence, and its
  VM clone's name.

A failed or blocked target's result keeps why it stopped, in the guest's
words: MacPorts' last errors, or the changed dependency that didn't pass.
`dockhand logs` shows it beside the outcome, and `--json` as `detail`.

Each target's result keeps what its build read: the ports active as it
built, each with its version, variants, directory, and archive's digest,
and the digest of the archive the build made. The guest asks `port` for
them once per target, after its verdict. They are what a later check will
compare to reuse the result (decision 28).

A port MacPorts fetches with Git (`fetch.type git`) names its source by
`git.branch`, usually a tag, which binds nothing as an archive's checksums
do: a project can move it. So a check resolves each such port's
`git.branch` to a commit as it is planned, from the repository's refs
(`git ls-remote`), and `check --plan` says which. The Portfile keeps its
tag. Once `port fetch` has cloned the port into its `worksrcpath`, the
guest reads the commit checked out there with `git rev-parse` and reports
it with the result, which keeps it with what the build read. A fetch that
checked out another commit than the check expected fetched another
source, one the tag names since the check was planned: the guest builds
nothing from it, and the target fails at fetch, saying so. That is a
verdict, not trouble with the guest, since another attempt would fetch the
same; a new check expects what the tag names then. An earlier build of a
Git-fetched port is reused only where it recorded the commit the new check
expects, and a port built against it only where it was built against a
build of that commit. A checkout the guest can't read is said in the
target's log, and the build goes on, its source unknown: no later check
reuses it.

The guest finds the checkout at `worksrcpath`, which it reads from
MacPorts' own Tcl interface as it reads whether a port declares tests. The
Portfile reference documents `worksrcpath` as the full path to the port's
source, and Portfiles' own post-fetch steps find a Git clone there, but it
doesn't say in so many words that a Git fetch clones there: that is Base's
`portfetch.tcl`, alike in 2.11 and 2.12.

A passed target's archive is copied out of the guest and kept beside the
database, checked against that digest. A later check that reuses such a
target, and builds one that needs it, gives its guest the archive in an
archive site of MacPorts' own kind, `file:///var/tmp/dockhand-archives/`,
signed with dockhand's keys, which the guest's `pubkeys.conf` is told to
trust: RIPEMD-160 with an RSA key, as `pubkeys.conf` documents, and
signify's, as MacPorts' own site uses. MacPorts tries a local site first,
and installs the target from it rather than build it. A retry installs
what an earlier attempt finished the same way. The keys are
`~/.dockhand/ssh/archives.key` and `archives-rsa.pem`, made on first use.
Signing an archive as a site's entry, and the keys, are
`macports/binaryarchive`'s; the provider uploads the entry and configures
the guest's MacPorts to trust it.

The archives its dependencies were installed from are kept the same way,
each by the digest the guest reported of it as active, for the port it is
and the environment it was installed in, unless the port is one of the
check's targets, the branch's build of it (batch 90). A later check's guest
in that environment is given the kept archives of its targets' direct
dependencies, as MacPorts evaluates them in the revision, and of the ports
their earlier builds had active, the two newest of each port, in the same
site. MacPorts takes one whose name is the archive it would fetch, and
builds or fetches the rest as before: a Rust toolchain built from source on
a release MacPorts has no archives of it for builds once, not in each check.

Each target's log is copied into the check's log directory (`dockhand
logs`). When the guest's Command Line Tools, or its Xcode in an Xcode
image, differ from the facts table's row the plan was read with, the
check says so as a drift report, which never changes a result
(decision 10).

## Observed

Facts about Tart, the guests, and the images that the provider's code works
around, each with the date it was seen. Read them before simplifying the
code they explain.

### Tart and the Mac

- **The two-VM limit is the Mac's, whatever Tart home a VM runs from.**
  With two VMs running in dockhand's home, a start from another home was
  refused "The number of VMs exceeds the system limit", naming no VMs; the
  same start in dockhand's home added "(other running VMs: …)". So only
  the message's first part is matched, and your own VMs, counted from
  `TART_HOME` or `~/.tart`, take slots too (2026-09-24).
- **Dockhand runs Tart with `TART_HOME` set to its own home**, never yours,
  so a VM of yours can't block its listing. Tart makes a home's
  directories on any command, so a command that only reads doesn't run
  Tart on a home that isn't there yet.
- **A clone made by another database**, a scratch `DOCKHAND_DB` or a test
  run, is reported and kept: `clean` and serve remove only clones their
  own database made. Remove one by hand with
  `TART_HOME=~/.dockhand/tart tart delete <name>` (2026-09-27). Stopping
  and deleting a clone takes about 4.4 s.
- **`tart list` asks diskutil about each VM's disk.** It took about 30 s
  on an M1, and hung on disks diskimagesiod had left attached and not
  mounted (`hdiutil info` shows them), so every listing is bounded at two
  minutes (2026-10-07).
- **Upstream Tart issues still worked around** (as of 2026-09-27):
  - openai/tart#1345: `tart delete` of a running VM says "does not exist"
    and leaves it. #1350 fixed it on 2026-09-26, hours after 2.39.0 was
    tagged; until a release carries it, a delete is trusted only by the
    VM's absence from the listing.
  - #1346, `tart exec` failing once its control socket does; #1347,
    `tart exec` losing output on macOS 12 to 15 guests; and #1348, its
    buffered input. Guests are reached over SSH, never `tart exec`, but for
    setup's agent readiness probe.
  - #1353: a listing that races a delete fails; it is listed again.
  - `tart clone` onto a stopped VM's name replaces it silently, and Tart
    renames running VMs, so dockhand refuses a destination Tart lists and
    renames only stopped VMs.
  - #1344, fixed by #1349 in 2.39.0: before it, `tart list` failed while
    an ASIF VM ran, and on 2.37.0 a listing in an ASIF VM's first seconds
    failed ten of eleven starts; on 2.39.0 eleven of eleven succeeded.
    `tart get` now reads an ASIF disk's capacity as `null` (2026-09-27).
- **Local Network privacy stops a Go program's direct dial to a guest**
  ("no route to host"), whichever app launched it, but not Apple's
  `/usr/bin/ssh`, which is why the guest channel uses it. Its
  ControlMaster socket is under `/tmp`, since the per-user temporary
  directory's path is too long for a socket. 64 MiB moved up in 0.9 s and
  down in 0.4 s on Monterey (2026-09-24).

### Images and guests

- **A raw disk's recovery partition sits after the main container**, so
  `tart set --disk-size` alone doesn't let the guest grow it. Setup removes
  the partition from the stopped clone's `disk.img`, the route Tart's FAQ
  points to and the one place dockhand edits a file inside a Tart home.
- **Golden Gate's vanilla image** was 30.9 GB compressed, a 50 GB ASIF
  disk taking 35 GB on the host, holding a 44.1 GB container and a 5.4 GB
  recovery partition with 10 GB free. On a Tahoe host the disk is a
  standalone `disk.img`, not the stacked `overlay.asif` a macOS 27 host
  could make. It had passwordless sudo and no Tart guest agent. Its base
  image took 2 min 40 s to set up, 41 GB on the host, and its Xcode image
  11 min 56 s, 47 GB (2026-09-27).
- **Reading a vanilla image's digest** from the registry takes 100 to
  200 ms (2026-09-27).
- **The guests' login shell is zsh, which reserves `status`**: a guest
  script variable of that name breaks (2026-09-15).
- **The first-boot agent bootstrap** on Sonoma and Sequoia returned exit
  125 twice, and a retry succeeded each time; the cause is unknown, and
  setup waits it out, bounded (2026-09-15).
- **`tart stop` ends a guest that doesn't shut down in time**, losing what
  it hadn't written, so the guest is told to sync first.
- **`diskutil apfs resizeContainer` in a guest** failed once with -69606
  on Sequoia, leaving 17 GB where 60 were needed, and a plain retry
  passed; setup tries three times, five seconds apart (2026-10-16).
- **Monterey's `xip --expand` fails intermittently** with "Block-compressed
  payload operation failed": with Xcode 14.2 (2026-09-15) and with 14.0.1
  (2026-10-16), on archives that expand on the host, an upload verified
  byte for byte, and a guest that had expanded the same archive before. A
  rerun passed. The cause is unknown; setup compares the guest's copy with
  the host's by size and sha256 before expanding, and says which failed.
- **lang/gcc15 refuses Xcode 14.0.1** at pre-fetch, so on Monterey's
  Xcode image anything that pulls in libgcc15 fails (2026-10-18).

### xcodes

Observed on 2026-09-27:

- it keeps your Apple ID in `configuration.json` under
  `~/Library/Application Support/com.robotsandpencils.xcodes`, the
  password in the Keychain, and its session under
  `~/Library/HTTPStorages/xcodes`;
- signed out, it exits 1 at once, writing nothing: "Apple ID: Missing
  username or a password. Please try again.";
- without a terminal it prints nothing until the download ends;
- it builds the download in the system's temporary folder and moves it
  into `--directory` only when complete; 7.5 GB took about 4 minutes;
- it has no choice between Apple silicon's and the universal archive, and
  names a prerelease `Xcode-27.1.0-beta.2+…`, which setup never takes.

### Ports in guests

- **jq's own tests fail in a guest where it built first**: its destroot
  recompiles `src/main.o` as root, mode 0644, and the unprivileged
  `make check` can't write it again, "Operation not permitted". Seen on
  Monterey and Sonoma (2026-09-15, 2026-09-24) and in Tahoe's Xcode image
  (2026-09-28); it is jq's, not dockhand's, and the tests are advisory.
