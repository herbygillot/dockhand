# A Python project's port from PyPI, with its backend, dependencies and Python versions

A feature for v0.10.0, [#187](https://github.com/herbygillot/dockhand/issues/187), placed at the person's word ("Adopt for v0.10.0, and do `depends_lib-append`.", 2026-11-11 04:06Z, in the architecture thread). The issue says where it stands; this file is the design, architecture's of 2026-11-11, read at dockhand main 0b8ecbda5 and MacPorts' tree at 744df9d1 (counts) and 4429c9a7 (the ports quoted). It comes from field testing's py-cyclopts (2026-11-09), which `create` wrote in a form that had to be rewritten by hand.

## In short

- **`create pypi:NAME` fetches the release's sdist from PyPI**, as 1,912 of the tree's 2,315 py- ports do, in place of a GitHub archive.
- **It writes `python.pep517_backend`** from the sdist's pyproject.toml, and a build dependency for each backend plugin the python PortGroup doesn't add itself.
- **It writes a dependency line for each requirement it can match to a py- port by name,** inside the usual `if {${name} ne ${subport}}` block, and names the requirements it couldn't match.
- **`python.versions` is worked out, not defaulted:** the tree's Python versions that the release, the build tools and every matched dependency all have.
- **Everything written from a reading is marked unconfirmed,** as create's guesses are today. Each mark says what was read.
- **The name index it needs is the one [#33](https://github.com/herbygillot/dockhand/issues/33) needs.** I'd do #33 on it in the same release.

## What create does today

For a Python project create writes this, whatever the argument:

```
PortSystem          1.0
PortGroup           github 1.0
PortGroup           python 1.0

github.setup        o tool 2.0 v
github.tarball_from archive
name                py-tool
revision            0

# dockhand: unconfirmed, guessed from the build system
categories          python
…
# dockhand: unconfirmed, the python PortGroup's default; add the other versions it supports, and say whether it fetches from PyPI instead
python.versions     314
```

- **A `pypi:` argument is only a lookup.** `registry.Client.pypi` reads PyPI's `info.home_page`, `info.project_urls` and `info.version` (`registry/registry.go:112-145`), returns the repository link, and the PyPI name is dropped (`engine/create.go:470-476`). The version is GitHub's most recently published release; PyPI's is used only where GitHub has none (`:527-533`).
- **The port's name is `py-` and the repository's name** (`engine/create.go:132-143`), not the PyPI project's.
- **Nothing reads the requirements for create.** `ReadPyproject` parses `[project].dependencies` and `requires-python` and discards `build-system.requires` and the extras (`project/python.go:125-137`); create uses only the licence and description.
- **A project with a Cargo.toml, meson.build or CMakeLists.txt isn't "python" at all:** `Detect` tries those before pyproject.toml (`portcreate/portcreate.go:61-64`).
- **No test holds a whole Python Portfile.** The tests assert fragments (`portcreate_test.go:121-133`).

## The target

The hand-written py-cyclopts now in MacPorts' tree is what `create pypi:cyclopts --version 4.25.3` should come to, apart from prose a person writes and one dependency type: that port declares its runtime requirements with `depends_run-append`, and create writes `depends_lib-append`. This is the file the design aims at. Its values are taken from that port; I haven't read cyclopts's metadata from PyPI.

```
PortSystem          1.0
PortGroup           python 1.0

name                py-cyclopts
version             4.25.3
revision            0

# dockhand: unconfirmed, PyPI has a pure-Python wheel (py3-none-any) for 4.25.3
platforms           {darwin any}
supported_archs     noarch
# dockhand: unconfirmed, from pyproject.toml's license field
license             Apache-2
maintainers         {…} openmaintainer

description         Intuitive, easy CLIs based on type hints.
# dockhand: unconfirmed, write a longer description
long_description    {*}${description}

homepage            https://github.com/BrianPugh/cyclopts

checksums           rmd160  … \
                    sha256  … \
                    size    …

# dockhand: unconfirmed, the Python versions that requires-python (<its text>) admits and py-docstring_parser and py-rich_rst both have; a check builds 314 only
python.versions     310 311 312 313 314
python.pep517_backend hatch

if {${name} ne ${subport}} {
    depends_build-append \
                    port:py${python.version}-hatch-vcs

    # dockhand: unconfirmed, matched to ports by PyPI name; check each
    depends_lib-append \
                    port:py${python.version}-attrs \
                    port:py${python.version}-docstring_parser \
                    port:py${python.version}-rich \
                    port:py${python.version}-rich_rst

    if {${python.version} < 311} {
        depends_lib-append \
                    port:py${python.version}-tomli \
                    port:py${python.version}-typing_extensions
    }
}
```

There is no `categories` line: the python PortGroup sets `categories python` itself (python-1.0.tcl:27), and 1,463 py- ports append to it.

## The source

**For a `pypi:` argument the release is PyPI's.**
- **Version:** the one named with `--version` ([#186](https://github.com/herbygillot/dockhand/issues/186)), or PyPI's latest. Not GitHub's latest release.
- **File:** the release's sdist, from PyPI's file list for that version. `pypi.Client.Files` already reads the list (`pypi/pypi.go:40-71`).
- **Name:** `py-` and the PyPI name normalised as `project.NormalizeName` does (lower case, runs of `-`, `_` and `.` to `-`). `--name` still overrides.
- **`python.rootname`:** written only where the sdist's file stem differs from the name without `py-`, as 325 ports do (py-pycrdt-websocket has `python.rootname pycrdt_websocket`). One line then keeps `master_sites`, `distname` and livecheck in step (python-1.0.tcl:58-62).
- **No github PortGroup and no `master_sites`:** the PortGroup's PyPI defaults apply.
- **Checksums:** as today, by fetching what MacPorts evaluates from the written Portfile (`engine/create.go:265`). That fetch is the proof the source lines are right. PyPI's own sha256 for the file is compared with the one computed, and a difference refuses.

**Where PyPI has no sdist for the version** (wheels only), create writes today's GitHub form and says why.

**A GitHub URL as the argument keeps the GitHub archive.** The person chose it. The rest of this design still applies to it, read from the archive's pyproject.toml.

**Pure Python.** Where the release has a wheel tagged `none-any`, create writes `platforms {darwin any}` and `supported_archs noarch`, marked with that evidence. 1,775 of the 2,322 py- ports at master are noarch. With no such wheel it writes neither.

## The backend

`macports.PythonBackend` maps pyproject's `build-backend` to the PortGroup's value (`macports/python.go:209-226`). It gains the three the PortGroup knows and it doesn't: `pdm.backend`, `uv_build` and `scikit_build_core.build` (python-1.0.tcl:425-438).

- **setuptools, or no pyproject.toml:** no line. It is the PortGroup's default, and only 4 ports write it.
- **Any other known backend:** `python.pep517_backend <value>`.
- **A backend the PortGroup doesn't know:** no line, and a mark naming the module.
- **Backend plugins.** The PortGroup adds the backend's own port and `build`, `installer`, and for setuptools `wheel` (python-1.0.tcl:382-434). It adds no plugin. Each entry of `build-system.requires` that isn't one of those is matched like a dependency and written as `depends_build-append`. hatch-vcs is the common one.
- **Compiled backends** (maturin, meson, scikit): the backend line is written and a mark says what isn't: maturin's crates and any library the build links. For a `pypi:` argument Python is the build system whatever else the archive holds.

## Dependencies

**Where they're read.** For a PyPI release, the release's own metadata from PyPI (`requires_dist`, `requires_python`). That covers setup.py projects, dynamic dependencies and Poetry's tables, which pyproject's `[project]` table doesn't. For a GitHub archive, pyproject's `[project].dependencies`, with today's reader.

**Each requirement, in order:**
1. **Its marker is evaluated for macOS at each candidate Python version** (`Requirement.OnMacOS`, `project/python.go:24`). One that never applies is left out and said in the output. One that applies only below or only from some version goes in an inner `if {${python.version} < 311}` block. One that needs an extra is optional: left out and counted.
2. **It is matched to a port by name** in the index below. A match is a line.
3. **The port's version is tested against the requirement** with `project.Admits`. Where it isn't admitted the line is still written, and a mark says so: "py-attrs is at 22.1.0, which >=23.1 doesn't admit".
4. **No match:** nothing is written for it, and a mark above the block names it. Create's output gives the next step for each. Create doesn't make the missing ports itself.

**Runtime requirements are `depends_lib-append`, build requirements `depends_build-append`.** The person's choice (2026-11-11), over the `depends_run-append` this design first proposed. It is the tree's commoner form for a Python port's requirements (about 718 `depends_lib-append` statements to 389 `depends_run-append`). The hand-written py-cyclopts uses `depends_run-append`, so create's draft differs from it there.

## Python versions

`python.versions` is the set of versions that pass all of these:
- **The tree's convention is the floor.** The lowest version in the commonest `python.versions` line among py- ports, which is 310 today (`310 311 312 313 314`, 764 ports). The tree still carries python32 to python39, and a new port shouldn't target them.
- **The build tools have it.** py-build, py-installer and the backend's port each have a subport for it. py-hatchling starts at 310.
- **`requires-python` admits it.**
- **Every matched dependency has a subport for it.**

The mark says which of these bounded the result and that a check builds the default version only. For cyclopts that gives 310 to 314: py-docstring_parser and py-rich_rst have no 315.

## The name index

A map from a normalised PyPI project name to a py- port, built from the master dockhand last fetched:
- each `python/py-*` directory's name without `py-`, normalised;
- each literal `python.rootname` line, read with one grep over `python/`. Where a rootname and another port's directory name claim the same project, the rootname wins.

About 107 of the 325 rootnames differ from their directory's name even after normalising (py-zmq is pyzmq, py-yaml is pyyaml), which is why names alone aren't enough. The same pass reads each port's `python.versions`.

**A match is by name and is always said as one.** A rootname that isn't a literal isn't read. Nothing else in dockhand maps a PyPI name to a port today: update's `provider` searches only the Portfile's own dependencies (`macports/assess/pins.go:60-75`).

**#33 on the same index.** With it, update's notice for a new upstream requirement can say "which MacPorts has as py-X, not among the Portfile's dependencies", or that no port was found by name. That is #33's whole ask.

## What the person sees

Create's summary gains a line each for the source, the backend, the dependencies and the Python versions. Drafts, to be reviewed before they're built. Each says what was read and claims nothing that wasn't:

- `py-cyclopts 4.25.3, from PyPI's sdist cyclopts-4.25.3.tar.gz.`
- `Backend: hatchling, from the sdist's pyproject.toml. The build also needs hatch-vcs, declared as py-hatch-vcs.`
- `Dependencies: 6 of PyPI's 6 requirements for macOS matched to ports by name. 3 optional extras were left out.`
- `No py- port found by name for rich-rst. Create it with dockhand create pypi:rich-rst, or declare the port that provides it.`
- `Python versions: 310 to 314. py-docstring_parser and py-rich_rst have no 315. A check builds 314 only.`

`create --plan` downloads nothing. It shows the source, the dependencies and the Python versions from PyPI's metadata, and says the backend is read from the sdist when create runs.

## How it is built

- **One PyPI reader.** `internal/pypi` reads a release: its files, `requires_dist` and `requires_python`. `registry.Client.pypi` calls it in place of its own decoding.
- **`portcreate.Spec` gains a Python part:** rootname, backend, noarch, versions and what bounded them, build and run dependencies with their version condition, and the unmatched names. `Write` renders it through today's `mark`, so `Marked` and `Unconfirmed` read the new marks back. portcreate still does no I/O.
- **The index is a pure type in `internal/macports`,** built from names and lines handed to it. The engine fills it from the fetched master.
- **`createSpec` and `PlanCreate` share one function,** the plan skipping the fetch. That also closes [#171](https://github.com/herbygillot/dockhand/issues/171).
- **`ReadPyproject` keeps `build-system.requires`.**

## Tests

- **Golden Portfiles, the first for Python:** a hatch project with a plugin, conditional dependencies and noarch; a rootname case; a setuptools project with no pyproject.toml; an unmatched requirement; a wheel-only release falling back to GitHub.
- **The index:** rootname beats directory name; normalising (typing_extensions, jaraco.classes, PyYAML through `python.rootname pyyaml`).
- **The versions rule:** bounded by a dependency, by `requires-python`, by the floor.
- **End to end against the fake PyPI and GitHub,** including a checksum that differs from PyPI's.
- **The acceptance row for `create pypi:`** moves in the same commit.

## Size and placement

About a third of a batch, inferred from the pieces above and not measured. Placed in v0.10.0 at the person's word, after #186, which it uses. I'd put #33 in the same release, on the same index. It isn't core work.

## Not in this design

- maturin's crates and the libraries a compiled extension links.
- Extras, test dependencies and `test.run`.
- Creating the ports that are missing. Each is its own `create`.
- Private indexes, and Python 2.

## Not checked

- cyclopts's metadata on PyPI. The target file mirrors the hand-written port.
- Whether PyPI's source path takes every spelling of a project's name. The checksum fetch is the test.
- Whether a check would show a runtime requirement left undeclared, such as one with no port.
- The tree counts come from two checkouts (744df9d1 for most, master 4429c9a7 for noarch and categories) and are greps, not evaluations.

## Usage docs

- docs/usage/new-port.md: what `create pypi:NAME` writes, each new mark and what confirms it, the Python-versions rule, and the next step for a requirement with no port.
- The same page: `create --plan` for a Python project, and a pointer to `--version`.
- The release notes carry it as a new feature.
