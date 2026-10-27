# create: Cargo's programs read once, and create --plan

2026-10-27, Then's item 14, its CX-7 and C8-9 part.

- **One reading of a Cargo project's programs**: `project.CargoPrograms`,
  over a set of files, gives each package's `Programs`: the root's and
  its members', its [[bin]]s, and the one of its name where its
  directory has a program by Cargo's conventions. sourcecompare's
  `cargoBinaries` and create's `portcreate.Binaries` both read it, where
  create had read the root package alone and named it a program wherever
  no [[bin]] was declared.
- **create reads what that needs** (`portcreate.CargoFiles`, read by the
  engine through the forge's File API at the tag):
  - `src/main.rs`;
  - each workspace member's Cargo.toml and src/main.rs, a member named
    by its path. A glob, as crates/*, can't be listed file by file, so
    its members aren't read.

  A workspace's binaries are then in the destroot, and a library package
  is no program.
- **`create --plan`** prints the Portfile create would write
  (`Engine.PlanCreate`, which shares `createSpec` with Create), with
  placeholder checksums, and writes nothing, starting no branch. The
  category comes from the person's checkout. contract_test's noPlan
  entry for create, "Later: …", is gone.
- Tests: TestANewPortInstallsWhatItsManifestNames (a library, a
  workspace's members, CargoFiles) and
  TestCreatePlanPrintsThePortfileAndWritesNothing.
