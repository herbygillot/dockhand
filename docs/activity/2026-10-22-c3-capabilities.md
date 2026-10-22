# C3: every capability explicit

Roadmap part 2, item 1's first step (with CX-5). The engine found what its
port reader could do by type assertion at seven sites, and did without a
capability it didn't find, mostly unsaid: no change records, direct
dependencies alone, nothing collected, subports unnamed.

- `engine.Ports` holds a field for each capability: `Reader`,
  `Families`, `Graphs`, `Archives`, `Fetcher`, `Names`, `Dependents`, and
  `Maintainers`. The engine's `PortReader`, `ArchivePlanner`,
  `ArchiveFetcher`, and `DependentReader` fields are gone. The narrow
  interfaces stay as the fields' types; `dependencyGraphReader` and
  `portNamer` are exported as `DependencyGraphReader` and `PortNamer`.
- With no `Reader` given, every field is MacPorts' own, made once from
  the concrete `treequery.Reader` (`MacPortsPorts`), so none is found by
  assertion; a capability given beside no reader stands in for MacPorts'
  (a test's archive planner). Given a reader, only the fields given are
  there.
- A missing capability is said or is an error where it's needed: no
  change records, the baseline's ports alone, and ordering by direct
  dependencies alone are each said with -v; fetching archives,
  assessing, and dependents refuse as before.
- `compose.Dependencies` carries `Ports` in place of `PortReader` and
  `DependentReader`; its `ArchiveFetcher` fills `Ports.Fetcher`.
- Tests say what their fakes read: `usePorts` gives a fake reader with
  each capability it has, and `useDependents` a dependents fake with the
  names it gives, none where it gives none, as the engine did when the
  dependents fake was the one asked for names.

Not done here, within C3: where it was decided. The roadmap says compose
fills each field; MacPorts' own reader is made by the engine on first
use, as before, since it reads the tree; compose fills what a test gives.
The forge's `renamed()` assertion is next, then `throughUnchanged`'s move
to `planning`.

## The forge's description, asked

Rename detection found by type assertion whether a forge's repository
could describe itself (`forge.DescribedRepository`), and with a forge
whose couldn't, found no rename, unsaid. `engine.Forge` now has
`Describe(ctx, instance, name)`, which the GitHub client answers through
its repository and forgetest by `Renamed`; `renamed()` and create's
project inspection ask it, and `DescribedRepository` is gone.

## throughUnchanged's traversal, planning's

`planning.ThroughUnchanged` walks the dependency graph, adding to each
candidate's evaluated dependencies the candidates it reaches through
ports the plan doesn't build, beside `Needs` and `Narrow`'s kin, with a
test of its own; the engine's `throughUnchanged` reads the graph, once,
where there are two candidates, and hands it over. With this, C3 is done.
