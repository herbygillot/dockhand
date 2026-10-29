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

For ports with `cargo.crates` or `go.vendors`, an ordinary refresh preserves
those lists byte for byte while refreshing the port's own archives. An empty
declared dependency block can be filled from the source. A declaration or
context Dockhand cannot handle is left unchanged and explained; where possible,
the command prints checksum lines for you to write yourself. Read that output
before treating the refresh as complete.

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
dockhand rename goplus xgo --plan
dockhand rename goplus xgo
dockhand update xgo
```

The new name is set on the Portfile's `name` line, or, where the name comes
from a setup such as `github.setup` or `go.setup`, on a `name` line below it, so
the port's source stays where it is. Pointing the setup at a renamed repository
is `update --follow-moves`. The stub uses
`PortGroup obsolete 1.0` and `replaced_by xgo`, at the old version with its
revision raised. Dockhand does only that much. It lists the new Portfile's lines
that still name the old name, such as a Go module path or a binary's name, and
the ports that depend on the old name; those changes are yours. `--category`
places the new directory in another category. `tidy` commits the rename as
`xgo: rename from goplus`.

An update whose GitHub repository was renamed under the same owner, where the
port is named after it or the project's own name changed with it, is held, its
line leading with `dockhand rename`. `update --follow-moves` still follows it,
keeping the port's name, but a port's `moves = "follow"` setting and `serve`
never do.

To move a port to another category without renaming it:

```sh
dockhand move dockhand macports
```

The move names the new category first on the port's `categories` line where the
old one was first, and `tidy` commits it as `dockhand: move to macports`.

## Finish an update Dockhand refused

First read the refusal and the planned diff:

```sh
dockhand update jq --plan
```

A refusal about unintended changes means Dockhand could not prove that its
proposed edit changes only what it intended. Inspect the Portfile and its
subports before proceeding. Use `--shared-release` only when the other subports
really share the release and you intend to update them together; it is not a
general override for fidelity failures.

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

Replace the sample message with your reason. `tidy` includes the uncommitted
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
