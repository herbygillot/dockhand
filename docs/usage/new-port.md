# Creating a new port

Use `create` to turn a supported upstream project into a first Portfile, then
review the packaging decisions and build it. The generated file is a draft:
successful generation does not establish that its license, installed files,
dependencies, or category are correct.

## Preview the Portfile before starting a branch

Suppose you want to package a project as a new port named `mytool`, under
`devel`. Replace `OWNER/PROJECT` and the sample port name with yours:

```sh
dockhand create https://github.com/OWNER/PROJECT \
  --name mytool --category devel --plan
```

The output identifies the release and build system, prints the proposed
Portfile, and lists the fields needing confirmation. Checksums are placeholders
in this preview. It writes no Portfile and starts no branch; it still reads
upstream metadata.

If the project or release is wrong, stop here. `create` currently writes ports
from GitHub projects. Direct GitHub input needs a published release; draft
releases and prereleases are excluded. It recognizes Cargo, Go, CMake, Meson,
and Python projects, but an unfamiliar or complicated build may need a Portfile
written by hand.

You can also identify a project through its registry:

```sh
dockhand create pypi:NAME --plan
dockhand create crates:NAME --plan
dockhand create go:MODULE --plan
```

The registry supplies the source repository. A project published to its registry
can use that published version's Git tag when it has no GitHub release. Registry
lookup can identify GitLab or Codeberg sources, but `create` currently refuses
those sources rather than generating their Portfiles. A missing or unsuitable
source link needs your own investigation.

## Create it in a worktree of its own

Once the preview is suitable:

```sh
dockhand create https://github.com/OWNER/PROJECT \
  --name mytool --category devel --new=mytool
cd "$(dockhand path mytool)"
dockhand edit mytool --no-open
dockhand diff
```

Dockhand writes `devel/mytool/Portfile`, stages it, fills in the initial
checksums where it can, and records the subject `mytool: new port` for `tidy`.
Nothing is committed yet. The staged file is included in a check run from this
worktree.

Choose the port name deliberately with `--name`. An existing name on master,
including a subport's name, is refused before `--new` starts a branch. Choose
another name if it represents different software; use the existing port's branch
workflow if you meant to update that port.

Use `--category` to choose its directory. Otherwise a terminal asks for a
category, while a script gets a marked guess. Before the new port is committed,
rerunning `create` in this branch with another `--category` moves its directory
and preserves your edits. `--branch <name>` can place a new port in an existing
tracked branch; keep unrelated new ports in separate branches unless they belong
in one change. `dockhand usage branches` covers those choices.

## Resolve the draft's packaging decisions

Open the printed Portfile path and read the complete file. For every
`# dockhand: unconfirmed` marker, check the value, correct it if needed, then
remove that marker. Submission holds while these guesses remain unconfirmed.

In particular, verify:

- **License and description.** The license can come from Cargo.toml or
  pyproject.toml, otherwise GitHub's detection. Confirm it against the source
  and use MacPorts' license names. Make the long description useful to someone
  deciding whether to install the port.
- **Maintainer and category.** The configured maintainer is used; without one,
  the draft has a marked `nomaintainer`. Choose the maintainer line you intend
  to take responsibility for. The command may suggest a line from your existing
  ports, but writes no configuration for you.
- **Build and installed files.** Check the selected PortGroups and build
  options. A Rust or Go draft includes a guessed `destroot`: review what it
  installs, executable names, data files, and documentation. Neither PortGroup
  installs the program for you.
- **Dependencies and source layout.** Check that the declarations cover the
  software's build and runtime needs, including native libraries. Generated
  language dependency lists do not settle those packaging decisions.

Rust crates are read from Cargo.lock. The draft marks crates from unsupported
registries or Git sources instead of inventing their entries. Program discovery
includes explicit binaries and `src/main.rs` in packages and explicitly named
workspace members; workspace members named by a glob such as `crates/*` are not
read. Review their programs yourself.

For a root Go module, the checksum step can fill an empty `go.vendors` through
`go2port`. A module below its repository root, such as
`go:golang.org/x/tools/gopls`, uses the module's version and subdirectory tag.
Its draft builds in that directory and marks `go.offline_build no` for review,
because generation of the vendored list assumes a root module. A Python draft
starts from the Python PortGroup's evaluated default version; verify the Python
versions and dependencies the project actually supports.

## Finish checksums, then build the reviewed draft

If creation reports a checksum or dependency-generation problem, the staged
draft can already exist. It exits with attention status 3 in that case. Stay in
its branch, repair the reported problem, and finish it with:

```sh
dockhand checksums mytool --plan
dockhand checksums mytool
dockhand diff
```

Do not treat placeholder checksums or a marked unfetched crate as a completed
draft. Once its source, dependencies, and unconfirmed fields are settled:

```sh
dockhand tidy --plan
dockhand tidy
dockhand check
dockhand submit --plan
```

Review the tidy plan before applying it; manual changes can require approval of
that plan. Run these commands inside the worktree so the edit being checked is
the one you just inspected. Examine the installed files and exercise the
program's basic functionality as appropriate; a passing build alone does not
confirm your packaging guesses.

Use `dockhand usage checks` to choose environments and interpret failures,
`dockhand usage publishing` to open the pull request, and
`dockhand usage recovery` to resume after an interrupted or failed step.
