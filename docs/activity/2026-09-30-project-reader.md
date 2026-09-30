# 2026-09-30: the project reader (item 9, step 1)

The [assessment design](../assessment-design.md)'s first step: what an upstream project's own files say is read in one place, `internal/project`, which knows nothing of MacPorts, and the comparison reads each version where its port builds.

## What moved

- **Into `project`:**
  - PEP 440 and 508, unchanged;
  - the build-system identities, `project.System`, out of `macports.BuildSystem`. `macports` keeps which PortGroups use which, now mapped onto `project`'s;
  - which files are license files, build files, and manifests, and which system each belongs to;
  - typed records of each manifest, where `sourcecompare` had reduced them to name-and-version maps:
    - `CargoManifest`: the package, its binaries, and each dependency with its table, Git source and pin, path, and whether it's optional;
    - `ReadCargoLock`, out of `macports/dependency`, with `CrateName`;
    - `GoModule`: the go directive, the requirements with which are indirect, and `Binary`, replacing `dependency.GoRequirement` and `GoBinary`;
    - `Pyproject`: `[project]`, its optional groups and `requires-python`, the build system's requirements and backend, and Poetry's table;
    - requirements files and `package.json`.
- **Onto `project`'s readers:**
  - `newport.Declare` and `Binaries`;
  - `newport.CargoCrates`;
  - `dependency`'s Cargo generation;
  - `portedit`'s Go toolchain check.
- **`sourcecompare`** now diffs two `project.Reading`s. `readManifest` projects the typed records into what it compares, keeping every rendering the old readers produced. It imports only `project`, no longer `macports` or `dependency`.
- **`macports.SourceSubdirectory`:** the directory below a distfile's top that a port builds in, from its worksrcdir, with `GOPATHLayout` moved beside it. `dependency.Manifest` uses it, as the engine does.
- **Boundary tests:** the engine names `project` among its imports, with why. A new test holds `project` to importing only `archive` of dockhand's own packages.

## What changed

`project.Read` decides where the project is before reading it (the update-workflow review's finding 1, batch 19's second item):
- **An enclosed archive** is read as before, below its one directory.
- **A flat archive** is read at its root. A changed root-level LICENSE had compared as nothing.
- **Several top directories and no file beside them** say nothing about which is the project. Nothing is read, and the comparison holds: "upstream's new archive holds a-2, b-2 and no file beside them, so which is the project wasn't found, and it wasn't compared". Earlier, the first directory met was read and the rest ignored. macOS resource forks don't count toward the layout.
- **Each version is read where its port builds,** from the evaluated worksrcdir's subdirectory. A monorepo's `python/pyproject.toml` is compared when the port builds in `python/`, and a manifest outside it isn't. License files are read both at the top and at that root, since a monorepo's license is usually at the top.
- **A subdirectory the archive lacks** is read at the top, as before, and named in the reading (`Missing`). It doesn't hold: a port's other distfiles lack its subdirectory too. Whether it's a gap is for `assess`, in step 2.

Three smaller changes:
- **Error prefix:** an error reading a Cargo.lock says `project:`, not `dependency:`.
- **Unparsed entries:** `[build-system]` requirements and optional groups, which weren't read before, keep an entry that isn't a requirement as unparsed rather than failing the reading. A bad entry among the project's own dependencies still fails it, as before.
- **`create` on a malformed manifest:** `Declare` and `Binaries` now give nothing for a Cargo.toml whose dependency table is malformed, or a pyproject.toml whose dependencies are. Before, they gave nothing only when the TOML itself was malformed. Such a manifest is rare, and what it would have given was a guess marked unconfirmed.

## Not in this step

- **Keeping readings** by what they read, and the fixture that the same read is reused, go with step 3, which keys them.
- **Reading a forge's archive of a commit** also goes with step 3.
- **`dependency.Manifest`'s own member search** stays for the generators. It and `project.Read` now share the subdirectory rule, and can share more once the generators read through `project`.

## Proven

- **Checks:** the full suite, `go vet`, `make fmt-check`, `vendor-check`, `deadcode`, and `lint` pass.
- **Unchanged behavior:** every existing `sourcecompare` test passes unchanged except the one quoting the Cargo.lock error's prefix.
- **New tests:**
  - layouts: enclosed, flat, ambiguous, and a subdirectory, including one archive read at two roots, `cli/` and `bindings/python/`;
  - a missing subdirectory, and truncation;
  - the Cargo manifest's records;
  - the pyproject's build system and unparsed entries;
  - go.mod's binary and lax reading;
  - a name first declared in one Cargo table, and path dependencies;
  - flat and ambiguous archives compared;
  - the engine comparing a nested project where its port builds.
- **Mutation testing:** every mutant of `project.Read`'s decisions is killed: layout, ignorable entries, license depth, root, subdirectory presence, and truncation. So is every mutant of the comparison's projection and the engine's wiring, once three tests were added for the ones that first survived.
