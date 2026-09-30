# 2026-09-30: a dependency met by a file closes no cycle

Item 6's fourth piece listed three more things after planning's phases: a dependency Base would find met by a file, the port reader's evaluation report, and the Base version on the bound probe. This note covers the first. It also says why the other two weren't done, since the code shows neither has a reader.

## What Base does

Base (`0c70cb739`, `src/port1.0/portutil.tcl` and `src/macports1.0/macports.tcl`):
- `_mportispresent` finds a dependency met by its port's receipt. Failing that, it looks for the file a `lib:`, `bin:`, or `path:` entry names:
  - `_bintest` searches `$env(PATH)`, so `/usr/bin` counts;
  - `_libtest` searches `/lib`, `/usr/lib`, the prefix's `lib`, and the system's framework directories;
  - `_pathtest` checks one path, relative to the prefix unless absolute.
- `_get_dep_port` then drops a dependency whose file is there and owned by no port.

Whether the file is there is the installing machine's fact. A plan made on the host can't know it for a guest: the Command Line Tools' `git` meets `bin:git:git`, and a prefix path is met only once some port puts it there.

## What the plan does now

`macports.Dependency.MetByFile` names Base's rule. `planning.Evaluate` keeps each dependency once, marked `ByFile` only where no entry names that port with `port:`.

Ordering by such a dependency stays as it was. Building the provider first is what Base does where the file isn't there, and costs nothing where it is.

What changes is a loop. Where the targets one environment builds loop, and one of the loop's dependencies is met by a file, that dependency is dropped and the order is found again. Before, the whole plan was refused as a cycle. Base drops that dependency where the file is there; where it isn't, Base would loop too, and the Portfile is wrong either way. A loop of dependencies by port alone is refused as before.

Still as it was: a target that needs a prerequisite needing Xcode, through a dependency met by a file, is unmet with the Command Line Tools alone. Whether the file would meet it there is the guest's to say.

## What wasn't done, and why

**The port reader's evaluation report, recorded.** The architecture review asked for it on one condition: that per-target reuse keyed by recorded inputs would need it, since "the ledger cannot be reconstructed from the port names and dependency lists later". Reuse has landed, and it doesn't need it. `reuse.Choose` keys a target on the inputs its build recorded in the guest (`model.TargetInputs`: the active ports' archive digests, the target's directory and `_resources` by tree, and the environment's identity), never on the host's evaluation. A recorded report would be a field with no reader, which finding 36 counts as a defect. It is taken off the order. If a reader appears, planning is now the one place that reads the evaluation, so adding a report there is simple.

**The Base version on the bound probe.** `Snapshot.Runtime.BaseVersion` already carries it once per evaluation. The per-port `dockhand.base_version` has two readers: the words of `ArchiveCompatible`'s refusal, and upstream's User-Agent. Both hold a `PortInfo` and no snapshot. Moving the version means passing the snapshot to both, to change nothing either of them does. It is taken off the order too.

## Tests

- `TestADependencyMetByAFileClosesNoCycle` covers a loop closed by a `bin:`-style dependency, dropped with the order kept, and an ordering by a file where nothing loops. It also covers the same loop by port alone, refused.
- `TestEvaluateReadsWhatPlanningNeeds` covers a port named by `port:` and `path:`, which counts as by port, and one by `bin:` alone.
- `TestADependencyIsReadAsBaseReadsIt` covers `MetByFile` for every form.

Six mutations each fail a test.
