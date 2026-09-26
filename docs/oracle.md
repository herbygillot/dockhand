# The oracle: scope for the local Mac work

Roadmap step 8. This is the scope settled on 2026-09-25, for a session on a
Mac with MacPorts Base, where the modelling data already collected lives.
Every phase changes the evaluator's Tcl and is proven by a survey, so none of
it can be built or checked in a Linux container. The design below was
settled with the person on the Mac the same day, and replaces the five
open decisions the scope left.

Sources: the direction record's "MacPorts in a controlled room" and decisions
9–13, 17, 18, and 35 ([contracts direction](reviews/2026-09-23-contracts-direction.md)),
its 2026-09-23 host inventory (`~/.dockhand/surveys/2026-09-23-host-inventory/`
on the Mac), and the [Base design prospective](macports-base-design-prospective.md)
§§4–6.

## What it is

MacPorts Base stays the interpreter. In the worker, every command that can
reach the host is hidden with `interp hide` and aliased to one dispatcher:
`exec`, `file`, `open`, `source`, `glob`, `readdir`, `::env`, pextlib, and
`registry::`. The dispatcher knows the context and answers from exactly one
source:

- **the captured tree**, for reads inside the port tree;
- **the host**, in native contexts only, for reads and a table of read-only
  programs;
- **the platform model**, in modelled contexts;
- **refusal**, for writes, network, and programs not in the table.

It records each answer with its source. A refusal goes into a ledger
outside the Portfile's control flow, which `catch` can't erase, and it
makes the context inconclusive rather than guessed.

It replaces four mechanisms (located on the Mac, 2026-09-25):

- **Platform overrides.** The platform variables overridden after init:
  `::macports::override_vars`, applied in `observation.tcl`
  (`observation_setup`) and `evaluator.tcl` (`model_platform`) from
  `macports.PlatformVariables`. `platform.tcl` is not this: it is the
  operand capture that profiles need, and it stays, or moves into the
  dispatcher.
- **Execution traces.** The traces in `observation.tcl` that record only
  with a Portfile frame on the stack, and only after `PortSystem`.
- **The stand-in `file`.** The Linux model's stand-in `file` in
  `evaluator.tcl` (`model_worker`), with `ModelVariables`, used only off a
  Mac.
- **The hook grammar's tables.** Hooks stay judged statically (decision
  17). The grammar has no program list: it refuses `exec` and `system`
  outright, and admits commands by table (`fetchguard`'s
  `hostReadCommands`, `harmlessBuiltins`, `fileReads`). Phase 2's
  allowlist of read-only programs becomes a new table the two share, not
  one taken over from the grammar.

## Why v3 needs it

1. **Plans the builder would make.**
   - Modelled contexts take this Mac's toolchain; Base asks for it in
     about 22,000 ports.
   - Native contexts read the person's registry and prefix, where a Tart
     guest or a GitHub runner starts from an empty prefix.
2. **Reuse (step 9).** Per-target reuse is keyed on recorded reads. Until
   every read passes one dispatcher, all of `_resources` counts as read.
3. **Safety.** Top-level Portfile code runs `exec` on the host today.
4. **Coverage.** 674 ports read host state; about 275 are worth
   modelling, pyqt5 first.

## The design

### Facts, by domain and source

Every answer the dispatcher gives is a fact from one of three domains:

- **toolchain:** Xcode, the Command Line Tools, SDKs, compilers, and
  `java_home`;
- **installation:** what the MacPorts registry has installed, and files
  in the prefix;
- **system:** `sysctl`, users and groups, and OS details.

A context picks one source per domain, so the phases move independently:

| Source | Domain | Answers from |
|---|---|---|
| `host` | any | the real commands the dispatcher hid |
| `table` | toolchain | the facts table (roadmap step 6) |
| `fresh` | installation | an empty registry, and a prefix holding only MacPorts |
| `with-deps` | installation | entries derived from the tree, below |

A model source is data handed to the worker when it is set up: a row of
the facts table, or a list of registry entries. The RPC runs from Go to
Tcl only, so Tcl can't ask Go mid-evaluation. The ledger records which
source gave each answer, and `host-in-model` is simply a modelled
context whose toolchain source is still `host`.

The name is *facts*, not a registry of them: "registry" is MacPorts' name
for its database of installed ports.

### A clean context is CI's

"Independent of host state" means what MacPorts CI starts from, since
that is what dockhand's evidence claims to match. CI evaluates a port
twice:

- `mpbb install-dependencies` evaluates it in an empty prefix, to find
  its dependencies;
- `mpbb install-port` evaluates it again once they are installed, and
  that evaluation is the one the build uses.

So there are two clean scenarios for installation, `fresh` and
`with-deps`. `fresh` is the default in every context, native included.
The person's own registry and prefix are never used for planning, only
for an explicitly local diagnostic.

### Registry questions at the checksum bump

A Portfile never opens the registry itself. It asks through Base's
aliases in the worker (`registry_active`, `registry_installed`,
`registry_exists`, `registry_file_registered`, `_portnameactive`), and
PortGroups build on those. A model has to reproduce Base's answers
exactly, including the error `registry_active` throws for a port that
isn't active: Portfiles rely on it, as in
`if {![catch {active_variants hdf5 cxx}]}`.

A registry answer matters to a bump only if it reaches a field the bump
edits: version, distfiles and master sites, checksums, the Go or Cargo
dependency blocks, or the revision. Most answers go elsewhere, to
dependencies, configure arguments, and paths. So for each port:

1. Evaluate with `fresh` answers, recording every registry question.
2. If there was one, rule it out statically where possible:
   `fetchguard`'s table of options that affect fetch, and `Tolerate`'s
   rule for PortGroup reads in harmless branch conditions. A branch that
   provably writes nothing fetch-relevant needs nothing more.
3. Otherwise evaluate again under `with-deps` and compare the edited
   fields. If they are equal, the answer doesn't matter to this bump. If
   they differ, the bump covers both, with checksums for the union of
   distfiles, as dockhand already does for distfiles that differ by
   platform, and the plan notes that this Portfile's fetch depends on
   what is installed.
4. What no scenario can answer, mainly which files a dependency installs,
   is inconclusive, and blocks only the edits that depend on it
   (decision 3).

`with-deps` is built only if it earns its place. Phase 4's survey runs
`fresh`, plus a pass over the ports that ask where every registry
question is answered "installed". If only a handful of checksum-relevant
results move, steps 1, 2, and 4 are enough.

### `with-deps`: derived, never installed

Nothing is installed anywhere before a check: not on the host, which
would pollute what is being isolated from, needs root, and takes hours
for a closure, and not in a Tart guest. `with-deps` is two passes of the
same evaluation:

1. **Empty registry.** It gives the port's dependencies on that platform,
   and its ledger names every port the Portfile asked about.
2. **Derivation, in Go.** For each asked-about port in the dependency
   closure: its version, revision, and epoch come from the per-platform
   port index already staged for this tree, and its variants are its
   default variants there, which is how CI installs dependencies. The
   index lists the variants a port offers, not its defaults (checked on
   hdf5), so defaults come from evaluating that port in the same
   context. An asked-about port outside the closure stays not installed,
   as CI's install would leave it: the other Qt5 flavours, for example.
3. **The entries.** The Portfile is evaluated again with them. If it asks
   about a port the first pass didn't, derivation runs again: a bounded
   fixed point, which also covers questions reachable only once an
   earlier answer was "installed".

The entries reach the worker as a table the dispatcher answers the
registry aliases from. Once phase 6 gives each evaluation its own
disposable `portdbpath`, they could instead be written into a small real
registry there, so that MacPorts answers every registry question itself.
That is higher fidelity, at the cost of writing through Base's internal
registry API, and is the step to take if surveys show registry reads
getting past the aliases.

A dependency's files are in neither the index nor the model. If the
survey shows file checks on dependencies changing results, the official
binary archive on `packages.macports.org` lists each package's files: a
download, not a build.

The real with-dependencies evaluation happens at `check` time. The Tart
provider follows CI in a fresh guest, installing the dependencies and
then evaluating and building the port. The guest returns the distfiles
and checksums of its own evaluation with the result, and a disagreement
with the plan becomes a finding, so every Tart build also checks the
model.

### Where derived answers live, and why they don't go stale

- **Derived from the tree being evaluated.** `registry_active qt5-qtbase`
  in a modelled Monterey context answers with what this tree's
  qt5-qtbase is on Monterey. When qt5-qtbase changes upstream, the next
  captured tree gives the new answer. There is no table of answers to
  refresh.
- **Cached by content.** Only the expensive part is kept across
  evaluations: a port's default variants per platform. It sits beside
  the port index in dockhand's disposable index cache
  (`~/Library/Caches/dockhand/indexes/`), keyed by the port's directory
  in the tree (Portfile and `files/`), the PortGroups it loads, the
  platform, and the MacPorts version. That is step 9's reuse by recorded
  inputs, and can share its machinery.
- **Never in `dockhand.db`.** The database holds records of work; this
  is data that can always be recomputed.
- **A survey's subject list only warms the cache.** If a new PortGroup
  starts asking about another port, the cost is a miss and one
  evaluation, never a wrong answer.
- **`java_home` is not a registry question.** The Java PortGroup runs
  `/usr/libexec/java_home -f -v ${java.version}`. Under `fresh` the
  answer is that no JDK is installed; under `with-deps`, it is where this
  tree's matching openjdk port installs its JDK, derived from its
  Portfile.

The facts table is different: it is stored data, and goes stale on
Apple's schedule rather than the tree's. Each row records the image or
builder it was harvested from, the date, and the MacPorts version that
read it. The table is checked in and shared, so a row can't name a
person's image by digest: staleness is found by drift instead. A guest's
facts are compared with its row (decision 10), and the table is
harvested again when Apple ships new tools or setup rebuilds an image,
as it did for Tahoe on 2026-09-25 ([harvesters](../tools/facts/README.md)).

### What the registry is asked

From the 2026-09-23 host inventory, every parse-time registry question
over the tree at `abd9fff84df`:

| Asked by | Subports | Portfiles |
|---|---|---|
| `qt5_version_info` | 836 | 186 |
| Portfiles directly | 57 | 14 |
| `active_variants` | 40 | 16 |
| `elisp` | 17 | 17 |
| all | 922 | 231 |

The inventory recorded 60 subjects. That is a lower bound: it kept one
record per worker for each command, not for each argument, so a worker
that asked about several ports recorded the first. `qt5_version_info`
recorded only qt56-qtbase, but its subjects are fixed in the PortGroup
(`available_qt_versions`): qt5-qtbase, qt513-qtbase, qt511-qtbase,
qt59-qtbase, qt58-qtbase, qt57-qtbase, qt56-qtbase, qt55-qtbase, and
qt53-qtbase.

The questions are of four kinds:

- **which version is installed:** `qt5_version_info` switches to an
  installed Qt5 flavour;
- **whether a dependency has a variant:** `active_variants` on hdf5,
  cairo, boost176, grass, csound, gtk3, geant4.10.5, hdf4, libepoxy, and
  scalapack;
- **conflicts and build order, in Portfiles:** qt5 about 25 of its own
  modules, py-automat and py-incremental about py*-twisted, star, smake,
  and cdrtools about cdrtools, py-qtpy and py-qt4py about the pyqt
  ports, gnupg2 about pinentry, and ldns and rpki-client about openssl;
- **who owns a file:** `elisp` asks `registry_file_registered` about
  the Emacs binaries in turn, `${prefix}/bin/emacs` first, then the two
  `Emacs.app` locations.

Phase 4's ledger records each question's subject, so every survey keeps
this list current. Its first survey found 93 subjects, asked by 897
subports.

### Isolation: best effort

There is no kernel sandbox. `sandbox-exec` is deprecated, and dockhand
depends only on official interfaces, even though MacPorts' own builds
use it. Nothing else fits: App Sandbox needs a signed app with an
entitlement, Endpoint Security an entitlement Apple grants to security
vendors, and a separate user account stops writes but not reads. A
2026-09-25 probe also showed why a sandbox alone would not do: a denied
`file exists` returned 0, a plausible "no clang", and evaluation carried
on with it.

So isolation comes from where evaluation runs, checked by comparison:

- **On the host, for the inner loop.** The dispatcher answers each
  question from its chosen source, and phase 2 refuses writes, network,
  and unlisted programs, logging each refusal. Phase 6 gives evaluation
  its own configuration, registry location, and `HOME`, so MacPorts'
  startup reads nothing of the person's. Evaluation runs as the person,
  so it cannot write to a root-owned prefix.
- **In a fresh Tart guest, as ground truth.** A fresh MacPorts has no
  host state to leak and the release's real toolchain. Evaluating a
  sample there and in the modelled context on the host, and comparing,
  finds leaks and modelling errors with evidence. It is an occasional
  check, not a gate. The guest also harvests the facts table.
- **If the comparison shows leaks below the Tcl command layer,** the
  next step is a Tcl virtual filesystem: a small extension using Tcl's
  public `Tcl_FSRegister`, loaded into the stock `port-tclsh`. It sees
  every path Tcl touches (`source`, `open`, `file`, `glob`, `load`),
  keeps frames intact, and can't be gone around by a Portfile. It still
  misses Pextlib's own system calls, which the aliases catch. It is
  built only on evidence.

What best effort gives up is a guarantee against a Portfile that goes
around the dispatcher on purpose, through `interp invokehidden` or C
code. Resisting malicious Portfiles was already out of scope, and a
Tart guest is the supported boundary if it ever matters.

**No custom `tclsh`.** MacPorts needs its C extensions (Pextlib, and
registry2.0 with SQLite) matched to each Base release, so a custom
interpreter means shipping a MacPorts runtime per release. It would
diverge from what CI evaluates with, and it was rejected as dockhand's
own Tcl evaluator.

## Phases

Each phase is proven by a whole-tree survey (`tools/survey`, about 41,800
ports), compared with the one before.

| # | Phase | Changes results | Acceptance |
|---|---|---|---|
| 1 | **Done.** Dispatcher in shadow mode: hide and alias, pass the host's answers through, count each call by its source. It replaces the trace-based observation. | 13 ports, a correction | The survey matches the baseline but for 13 ports whose host reads the old traces could not see ([note](activity/2026-09-25-oracle-phase-1.md)); the cost equals the traces'. |
| 2 | **Done.** Effects refused: writes, network, and unlisted programs, at the dispatcher in every worker, with a table of about 20 programs that only report (`macports.HostPrograms`), there for the hook grammar too. Each refusal is kept in the parent, out of `catch`'s reach, and the evaluation isn't trusted. A first comparison with a fresh Tart guest, on a sample, waits for the v3 Tart provider. | No, at this commit | No port attempts an effect that would take place ([note](activity/2026-09-25-oracle-phase-2.md)); the guest comparison's differences are explained. |
| 3 | Workspace rule (decision 35): a read inside the tree materialises on demand, and a read outside it is refused. | No | Evaluation reports the complete set of files it read. |
| 4 | **Done.** Installation `fresh`: an empty registry, the prefix holding only Base and its skeleton, programs looked up along MacPorts' `PATH` as a fresh prefix has them. The ledger records each question's subject. | Yes: 62 ports conclusive that weren't; dependencies in 2,830 contexts; no fetch field | Every changed result is explained ([note](activity/2026-09-26-oracle-phase-4.md)). |
| 4b | `with-deps`, if a survey shows a registry answer reaching what a bump edits. At `abd9fff84df` none does. | Only where fetch depends on what is installed | The moved ports are covered, or inconclusive for the fields affected. |
| 5 | **Done.** Toolchain from the facts table (step 6): Xcode and Command Line Tools versions, developer directory, compilers, and SDKs per release, in the tools profile. Replaces `ModelVariables`' fixed toolchain and the stand-in `file`. | Yes, in modelled contexts: 6 ports conclusive; fetch fields in 58 contexts of 19 Portfiles | Every changed result is explained ([note](activity/2026-09-26-oracle-phase-5.md)). |
| 5b | **Done.** The Java PortGroup's hook admitted (decision 18): an `exec` of a program that only reports, by `HostPrograms`, and `depends_${…}` judged by its family. `java_home`'s modelled answer is still to come. | Yes: 173 Java ports input-found, none regressed | The 180 Java ports come in, confirmed by the survey ([note](activity/2026-09-26-oracle-phase-5b.md)). |
| 6 | Bootstrap under the environment contract: `mportinit`'s `sw_vers`, `sysctl`, `xcodebuild`, its own `macports.conf`, a disposable `portdbpath`, and a `HOME` dockhand owns. May belong to step 6. | Possibly | — |

Out of scope:

- resisting malicious Portfiles; isolation is best effort, and a Tart
  guest is the boundary if that ever matters;
- dockhand's own Tcl evaluator, or a custom `tclsh`, rejected;
- running hooks inside the oracle, dropped by decision 17.

## Decisions

The five the scope left open, as settled on 2026-09-25:

1. **Modelled toolchain questions before the facts table.** The host
   answers them, and the ledger tags them `host-in-model`, flagged but
   not refused, until phase 5 switches the toolchain source to `table`.
   Phases 1–4 lose no coverage, and the tag counts what phase 5 must
   replace. This falls out of facts by domain and source.
2. **Phase 4's behaviour change.** Accepted: every context, native
   included, plans against a clean installation, whatever is installed
   on the Mac. Clean means CI's two scenarios, `fresh` by default and
   `with-deps` where it matters.
3. **How far "inconclusive" blocks.** As the prospective's §4 has it:
   only the edits that depend on the unresolved field. A refused
   `java_home` blocks a Java port's checksums, not its revbump.
4. **Performance.** At most 10% over untraced for shadow mode, with
   frames captured only for host-facing answers. Phase 1 costs what the
   old traces did.
5. **Proving phases without a person at the Mac.** Deferred. Surveys run
   where the person chooses, which today is this Mac. GitHub's macOS
   runners for bulk work are a later question.

Isolation, not among the five, is settled as best effort, above.

## Open questions

- **Which profile a modelled context is.** Phase 5 models the tools
  profile, dockhand's base images'. MacPorts' builders all have Xcode.
  The profile changes 15 Portfiles' local patch files and which contexts
  some guards refuse, but nothing a checksum bump edits at
  `abd9fff84df` ([note](activity/2026-09-26-oracle-phase-5.md#which-profile)).

- **Which variants `with-deps` installs.** CI installs a dependency with
  its default variants, and so does the model. Whether the person should
  be able to choose them instead, beyond a debugging flag, is open.
- **A Portfile whose fetch depends on what is installed.** The plan
  notes it. Whether dockhand should also raise it with the maintainer as
  a problem is open.
