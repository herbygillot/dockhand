# 2026-10-22: Part 6, the libraries a build links

Part 6 of v0.5.0: each Mach-O a port installs, its links under the prefix
mapped to the ports that own them, against the lib dependencies the port
declares, said as a finding, a notice first.

## Weighed first: `port -t` or reading the links

- **`port -t`**, MacPorts' trace mode, runs the build under darwintrace,
  which hides from it every file under the prefix that no declared
  dependency installed, and reports each access it denied. It says what
  the *build* reached for, not what the installed binaries link, and it
  changes the build it watches: an undeclared dependency fails or quietly
  configures without the feature, so the result isn't the build a check
  would otherwise make, and it is slower.
- **Reading the links** (`otool -L` on each Mach-O `port contents` lists,
  `port provides` mapping a linked path to its port) observes the build as
  it was, after it passed, with MacPorts' and Xcode's documented tools, and
  answers the question asked: what does the port link but not declare.
- **So reading the links is the mechanism**, and trace mode a complement
  for later, as a check mode of its own (`check --trace`), since it makes a
  different build. Neither is in the guest today: it lists the active
  ports (`consumed`), never a target's own files, and runs no otool.

## What it adds

1. **The guest** (guest.tcl), after a target passes: `port -q contents`
   of the target, `otool -L` on each file that is Mach-O, each link under
   `${prefix}` kept, `@rpath` links resolved through the file's
   `LC_RPATH`s (`otool -l`), and each kept path's owner by `port -q
   provides`. The result's new `links` is each linked port with the files
   that link it; absent where any step couldn't run, empty where nothing
   under the prefix is linked, as `active` is.
2. **One report value** (CX-10): a provider hands a target's report whole,
   `buildenv.TargetReport{Result, Fetched, Active, Links}`, nil kept apart
   from empty for each observation, to one `Build.Record`, where `Fetched`
   and `Consumed` were callbacks that had to come before it.
3. **The finding**: the engine sets the links beside the port's declared
   dependencies (the evaluated `PortInfo.Dependencies`, which keep each
   one's field), and says a port linked but declared in no `depends_lib`
   (nor the port itself, nor its subports): "jq links libonig.5.dylib from
   oniguruma, which it doesn't declare in depends_lib". A port declared in
   `depends_build` alone, and linked, is said too, since it may not be there
   at run time. It's a notice first, as the roadmap says: in the check's
   report and status's branch detail, holding nothing, with the ledger's
   finding kinds carrying it where a later part makes it hold.

## Placement (architecture, 2026-10-22)

- The guest gathers the links, an observation, not an input: an unknown
  doesn't stop reuse, and the finding says "couldn't read what the build
  links", never "links nothing".
- `buildenv.TargetReport{Result, Fetched, Active, Links}`, one `Record`,
  is the contract, with `Links` typed (binary, library, providing port).
- The rule is evidence's, `evidence.UndeclaredLinks(results, declared)`,
  the declared lib ports as plain names per target; `engine/links.go`
  only gathers: the results' links, the evaluated dependencies, the
  record.
- The links live with the result, as `Fetched` and `Active` do; the
  finding is a finding kind on part 2's ledger, its origin the check,
  holding nothing, shown from the ledger in the check's report and the
  branch detail, where a person can answer it with a decision ("a bundled
  copy, fine").

## CX-10: one report value

- `buildenv.Build.Record` takes a `buildenv.TargetReport`: the result,
  `Fetched` (empty where unknown), and `Active` (nil where unknown, empty
  where none), where `Consumed` and `Fetched` were callbacks that had to
  come before `Record`. `buildenv.Reported` is a result with nothing seen
  beside it. The engine's build records the observations, then the result
  (`build.Record`, `activeIn`, `fetchedBy`, `record`), as it did.
- Tart, the script provider, GitHub Actions, and the tests' fakes report so.

## What a build links, gathered and kept

- **The Tart guest** reads, after a target passes, what its installed
  Mach-O files link under the prefix (guest.tcl `links`): `port -q
  contents` lists its files, a file's magic says whether it's a Mach-O
  (thin, or universal, told from a Java class by its small count of
  architectures), `otool -L` lists its libraries, its own install name
  (`otool -D`) left out, an `@rpath` link resolved through its `LC_RPATH`s
  (`otool -l`), and an `@loader_path` or `@executable_path` one against its
  directory, each path cleaned lexically, as the loader reads it; `port
  provides`, asked once for every library, names each one's port. A list
  that can't be read leaves the links absent, and the log says so.
  `VerifierProtocol` stays 3, since nothing is built or judged otherwise;
  `guestPin` moves.
- **The host** reads them into `buildenv.TargetReport.Links`
  (`model.Link{Binary, Library, Port}`), and the engine keeps them with the
  result, `model.TargetResult.Links`, nil where unknown, never in its
  inputs, so they're no reuse key. A reused result keeps its earlier
  build's links.
- **Schema 39** adds `results.links`, JSON, NULL where unknown and an empty
  list where nothing links.
- The script provider and GitHub Actions report none, unknown.
