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
`dockhand providers setup tart tahoe --xcode <Xcode .xip, or a folder of them>`
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
capacity = 1          # checks serve runs at once; 1 when unset
test_timeout = "45m"  # a target's tests; 30 minutes when unset

[providers.tart.xcode]  # a release's Xcode image, by name or number
tahoe = "26.6"          # what MacPorts' arm64 buildbot runs when unset
```

macOS runs two VMs at most, yours among them, so a check waits for a slot
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

Each target's log is copied into the check's log directory (`dockhand
logs`). When the guest's Command Line Tools, or its Xcode in an Xcode
image, differ from the facts table's row the plan was read with, the
check says so as a drift report, which never changes a result
(decision 10).
