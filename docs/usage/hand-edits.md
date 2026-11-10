# Editing a port by hand

Use this workflow for a patch, a build fix, a dependency change, or a version
update you want to make yourself. Dockhand reads the branch's files as they
stand; you can edit them with any editor. Its authoring commands leave working
files for you to inspect. `tidy` makes the commits, `check` supplies build
evidence, and `submit` publishes them.

## Put the edit in the right worktree

For a new fix to jq, start a branch and ask for the Portfile's path:

```sh
dockhand edit jq --new=jq-fix --no-open
cd "$(dockhand path jq-fix)"
```

The first command brings jq's directory into the new worktree and prints its
Portfile's path. Open that file with your editor. Without `--no-open`, Dockhand
opens `$VISUAL` or `$EDITOR` when it has a terminal and an editor configured.

To continue an existing tracked branch, use `dockhand edit jq -b jq-fix` instead
of `--new`. Run subsequent commands in that branch's worktree. From elsewhere, a
branch with both commits and working edits needs an explicit capture choice. Use
`dockhand check -b jq-fix --working-tree` to check the edits, or `--head` for
the committed tip.

If you already made a Git branch yourself, run `dockhand adopt` in its checkout
first. Adoption tracks the branch without moving or rewriting your work. A
terminal may offer adoption when another command finds an untracked branch;
scripts should adopt explicitly. `dockhand usage branches` explains branch
selection and worktree placement.

For a new patch, put its file under the port's `files/` directory and name it in
`patchfiles`. The tidy plan says which untracked patches it will take and which
files it leaves out. For a check before committing, `check --include <path>` can
include an untracked file without staging it; do not assume every new file is
captured automatically.

## Refresh checksums after changing the source

After editing a version or a download location, inspect what a checksum refresh
would do:

```sh
dockhand diff
dockhand checksums jq --plan
dockhand checksums jq
dockhand diff
```

`checksums` fetches the distfiles the Portfile now names and writes their
checksums. It also looks at modeled platform contexts and variants with archives
of their own. The plan writes no edit and starts no branch; it can still
download files. It exits 3 when a distfile differs from its declared checksums,
so a nonzero plan result can be the difference you came to inspect.

**Other macOS releases and architectures.** A Portfile can fetch different
files on different releases or architectures, such as an older version kept for
macOS 10.13 and earlier, or a separate binary for Intel. When `update` or
`checksums` edits such a Portfile, Dockhand reads it as MacPorts would on each
side of every release or architecture boundary the Portfile names. It does this
on this Mac, without a build, and writes the checksums for every file those
readings fetch. Those readings use that release's version and architecture, and
its developer tools as Dockhand's facts table records them. Where a reading
depends on anything else about a Mac running that release, `checksums` leaves
that release's checksums as they were and names them, and `update` stops and
says which release it couldn't read. Checks build only on the releases you have
images for, or GitHub's. MacPorts' builders cover the rest after the pull
request is merged.

For ports with `cargo.crates` or `go.vendors`, an ordinary refresh preserves
those lists byte for byte while refreshing the port's own archives. An empty
declared dependency block can be filled from the source. A declaration or
context Dockhand cannot handle is left unchanged and explained; where possible,
the command prints checksum lines for you to write yourself. Read that output
before treating the refresh as complete.

A subport, or a main port, that declares no checksums for the archives it
fetches, as a port moved off a Git clone by hand, gets a checksums block
written and filled, and the refresh says so: `checksums: jq declared none for
jq-1.8.1.tar.gz, so they were written after its fetch lines and filled`.

A hand edit that leaves the Portfile unparsable, or changes its name line, is
said as that, not as an unknown port: `security/KeePassXC/Portfile doesn't
parse, so no port named keepassxc is in keepassxc-2.8.0's index: portindex says
invalid command name "versio". Fix the Portfile and run the command again`, or
`security/KeePassXC/Portfile names its port keepasxc now, where master names
it keepassxc. Restore the name line, or ask for keepasxc; a port renamed on
purpose goes through dockhand rename keepassxc keepasxc`.
A subport the edit dropped is said as dropped: `security/KeePassXC/Portfile
defines keepassxc now and not keepassxc-devel, which master has there too.
Restore the subport, or ask for keepassxc`. A Portfile the branch doesn't
change that still doesn't parse is said with where to look: `... doesn't parse
in keepassxc-2.8.0's index, though the branch doesn't change it: portindex says
... Check changes on the branch that this Portfile reads, such as PortGroups`.

A changed archive under the same name, with the branch's version unchanged, is a
**stealth update**. A homepage or comment edit does not turn it into a version
update. Dockhand reports the old and new checksums and compares the archive's
contents with what MacPorts shipped. Identical files in a repackaged archive
need no revision bump; otherwise it bumps the revision unless you already did
so. An unavailable content comparison is reported and keeps the revision bump.

The new mirror copy gets a separate `dist_subdir`, normally
`${name}/${version}_${revision}`. Use `--no-revbump` only when you have
established that installed copies need no rebuild; it leaves the revision and
numbers the mirror directory instead. If the revision or directory change would
affect other subports unexpectedly, Dockhand refreshes the checksums and leaves
that part for you, explaining why.

For a port using the github PortGroup, `update` can also change the source.
The default `--source suggest` reports suitable release assets for legacy
tarball or Git sources. It preserves the source, except that legacy tarballs
move to GitHub's archive format for the tag. For example, a notice may say
`release 2.5.0's mindforger_2.5.0_tarball.tgz carries the source, doc, and
deps/cmark-gfm; update --source releases, run instead of this update, fetches
it`. The move belongs to the update that changes the version: once the port is
at that version, `--source releases` doesn't move it yet.

`--source releases` requests a release asset and refuses one that does not
qualify. `--source choose` can move legacy tarballs or Git checkouts to a
suitable release asset; an asset replacing a Git checkout must contain every
submodule. Existing archive sources stay unless `--source releases` is
requested. `--source keep` preserves the source, and `--source archive`
requests GitHub's archive of the tag. Update doesn't move a port that already
fetches a release asset back to the archive, so `--source archive` there is
refused before anything changes: `--source archive: mindforger fetches a
release asset, and update doesn't move a port back to GitHub's archive of the
tag`.

When switching source, Dockhand sets `distname`, `extract.suffix`, a `use_`
option, and `worksrcdir` as needed, removes `fetch.type git` and the submodule
hook when replacing a Git checkout, and fills in checksums. The commit body
and pull request explain the move. `[update] source` sets the policy for
every update; see `dockhand usage configuration`.

## Request a rebuild without changing the version

When a patch or dependency change requires installed copies to rebuild, raise
the revision and give the reason:

```sh
dockhand revbump jq --subject "rebuild for a dependency change" --plan
dockhand revbump jq --subject "rebuild for a dependency change"
```

These commands use the branch checked out here. The plan shows the revision
change; the applied command records the reason for `tidy`'s commit subject. You
can name several ports, or use `--new=<name>` for a separate rebuild branch. A
revision shared by subports moves for all of them, so inspect the family in the
diff.

## Rename a port, or move it to another category

When a project renames itself, MacPorts renames its port: the Portfile and
`files/` move to the new name's directory, and the old Portfile becomes an
obsolete stub that names the new port.

```sh
dockhand rename jq jqlang --plan
dockhand rename jq jqlang
dockhand update jqlang
```

`--plan` starts no branch: with `--new`, or where no branch is checked out
here or changes the port, it plans on master as fetched now.

The new name is set on the Portfile's `name` line, or, where the name comes
from a setup such as `github.setup` or `go.setup`, on a `name` line below it, so
the port's source stays where it is. Pointing the setup at a renamed repository
is `update --follow-moves`. The stub uses
`PortGroup obsolete 1.0` and `replaced_by jqlang`, at the old version with its
revision raised. Dockhand does only that much. It lists the new Portfile's lines
that still name the old name, such as a Go module path or a binary's name, and
the ports that depend on the old name; those changes are yours. `--category`
places the new directory in another category. `tidy` commits the rename as
`jqlang: rename from jq`. A rename you make by hand is recognized too: a port
given a new name in its own directory, or moved to the new name's directory
with the stub left behind, is compared with its predecessor and its upstream
rather than read as a new port, and tidy names the rename with the update,
`zsolo2: rename from zsolo, update to 1.1`.

An update whose GitHub repository was renamed under the same owner, where the
port is named after it or the project's own name changed with it, is held, its
line naming `dockhand rename` before `update --follow-moves`.
`update --follow-moves` still follows it, keeping the port's name, but a
port's `moves = "follow"` setting and `serve` never do.

To move a port to another category without renaming it:

```sh
dockhand move dockhand macports
```

The move names the new category first on the port's `categories` line where the
old one was first, and `tidy` commits it as `dockhand: move to macports`. The
port is moved as committed: one with changes not committed is refused, `tidy
or commit them first`, and one the branch added and hasn't committed yet, as
`create` leaves it, is told `dockhand isn't committed yet; tidy it, then move
it`; tidy then folds the move into the port's commit.

## Finish an update Dockhand refused

First read the refusal and the planned diff:

```sh
dockhand update jq --plan
```

A refusal about unintended changes means Dockhand could not prove that its
proposed edit changes only what it intended. Inspect the Portfile and its
subports before proceeding. Use `--shared-release` only when the other subports
really share the release and you intend to update them together; it is not a
general override for fidelity failures. The plan also says whether the port's
patches apply to the new source, and, before them, a `worksrcdir` the source
lacks, `! worksrcdir sdk/angelscript/projects/gnuc macosx isn't in 2.38.0's
source`, which the patches' failures follow from.

If you want a starting point for a manual repair, run this in your chosen
branch:

```sh
dockhand update jq --hand-edit
```

Where the update can reach its edit attempt, this writes the best version
change, revision reset, and checksum refresh it can make. Read everything it
says it left undone. The result is your manual edit to finish, without
Dockhand's usual fidelity proof. The original refusal is recorded and shown in
status, the tidy plan, and the pull request description; unattended publication
holds on it. `ship`, `bump`, and `serve` do not take this path. Stop if you
cannot explain the collateral changes rather than repeatedly adding overrides.

## Review, commit, and check the finished edit

For this example's manual fix, supply a commit message that describes the actual
change:

```sh
dockhand diff
dockhand tidy --squash --message "jq: fix the build" --plan
dockhand tidy --squash --message "jq: fix the build"
dockhand check
dockhand submit --plan
```

Replace the sample message with your reason. Where the plan is one commit, as
it is for edits not yet committed to one port, `--message` alone does the same.
`tidy` includes the uncommitted
edit and may fix deterministic lint issues on lines you touched; inspect its
plan before applying it. A passing check supports the committed files you are
about to publish. Follow `dockhand usage publishing` for the submission itself,
or `dockhand usage recovery` if a check fails or the edit needs undoing.

## Try the port in your own MacPorts prefix

For a recorded local check, use `dockhand check --on local`. It builds in your
local MacPorts prefix and restores the prior active ports afterwards. `--keep`
deliberately leaves the branch's versions active. Read `dockhand usage checks`
before choosing that environment.

For a direct, unrecorded installation of this example's Portfile:

```sh
sudo /opt/local/bin/port -D "$PWD/textproc/jq" install
```

Run it from the branch's worktree root, and adjust the prefix and port directory
for your installation. This installs into your live prefix and leaves the result
there; it creates no Dockhand check evidence. `-D` selects that Portfile, but
dependency names still resolve through MacPorts' configured sources. It does not
make a dependency edited elsewhere in the same branch visible. Use a Dockhand
check for a branch with changed dependencies, or deliberately configure your own
temporary MacPorts source overlay and keep its port index current. Dockhand does
not add worktrees to your system's `sources.conf`.
