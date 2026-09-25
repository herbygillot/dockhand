# 2026-09-25: groundwork for the oracle

What the oracle ([scope](../oracle.md), roadmap step 8) needs before its
first phase, gathered on the Mac.

## The survey, back on main

Every oracle phase is proven by a whole-tree survey compared with the one
before. That was v2's `dockhand assess --all`, deleted with v2's command
line, and a `v2-final` binary can't stand in, since it lacks the
evaluator changes the oracle will make on `main`. `tools/survey` brings
it back as a developer tool beside `tools/stateperf`
([README](../../tools/survey/README.md)):

- **The same survey.** It wires `internal/assess` as v2's `app.Assess`
  did: MacPorts' evaluator through a staged, offline port index, and the
  go2port and cargo2port helpers.
- **The same journal**, so the old baselines compare directly, resumable,
  plus a `<journal>.run.json` of how the run was made and what it cost.
- **Comparison built in.** `-compare` replaces the ad-hoc Python used for
  the 2026-09-22 comparisons. It covers outcome moves, regressions,
  changed findings by pattern, fetch guards lost or changed, fetches
  refused then accepted, Portfiles fully covered, and where ports stop.
  Run on the 09-22 rerun and effect-rule journals, it reproduces that
  evening's figures: 176 unsupported to input-found, 78 to unknown, 272
  fetches refused then accepted.

**Parity.** `v2-final`'s `assess --category textproc` and
`tools/survey -category textproc` ran on the same commit (`abd9fff84df`),
MacPorts (`/opt/macports-test`, 2.12.6), and index cache. Their journals
match port for port on all 1,960 ports: outcomes, findings, fetch guards,
and Portfiles covered, with no moves.

**Which MacPorts.** The earlier baselines were run without `-p`, so they
evaluated with the first `port-tclsh` on PATH, `/opt/macports-test`'s, as
the host inventory's paths show. New baselines pin it with `-prefix`.

## Corrections to the scope

`docs/oracle.md` named `platform.tcl` as the platform overrides and gave
the hook grammar a program list; neither holds. The overrides are
`override_vars` in `observation.tcl` and `evaluator.tcl`, and
`platform.tcl` is the operand capture profiles need. The grammar refuses
`exec` and `system` outright and admits commands by table, so phase 2's
program allowlist is a new table for both, not one taken over.

## Kept

The instrumented evaluator behind the 2026-09-23 host inventory existed
only in `/tmp/dockhand-inventory`. It is now beside its data, in
`~/.dockhand/surveys/2026-09-23-host-inventory/instrumentation/`.

## The toolchain facts, Tahoe re-probed

The 09-24 facts found the Tahoe images on the macOS 27 Command Line
Tools. Rebuilt in step 2, they were probed again with the same scripts
(`~/.dockhand/surveys/2026-09-25-toolchain-facts-tahoe/`). Both images
now match the buildbot's `ports-26_arm64` builder:

- CLT 26.6.0.0.1781586589, and Xcode 26.6.
- SDK 26.5, and xcrun's default is 26.5 too.
- clang 2100.1.1.101, for Base and the shell alike.

The facts table (roadmap step 6) can start from these rows.

## What the dispatcher costs

Measured in `port-tclsh` (Tcl 8.6.17), per call:

| Call | Direct | Hidden and aliased to a dispatcher |
|---|---|---|
| a pure `file` subcommand (`join`) | 0.28 µs | 0.64 µs |
| a read inside the tree | 0.53 µs | 1.26 µs |
| a host read, recorded | 1.11 µs | 4.01 µs |
| a host read, recorded with the caller's frames, 20 deep | | 15.78 µs |

How many calls: opening a port in one context (`mportopen`, counted
with traces installed where the dispatcher would be, at
`worker_init leave`) makes about 170 of them. Nine ports were sampled,
from jq's 166 to tailscale's 188; apache-ant's two are
`exec /usr/libexec/java_home`. Of those 170:

- about 77 are pure `file` path arithmetic;
- about 51 are `source`;
- 30 are `::env` reads;
- 7 to 24 are other `file` calls;
- 2 are `open`.

A survey evaluates each port in several contexts, and spends about 560 ms
of CPU on a port all told (49 minutes on 8 cores for 41,800 ports).

The host inventory cost 28%: 62.6 minutes against 48.8 untraced. It
traced about 70 commands, every call, and walked every call's frames.
The table says where that went: routing a call through a dispatcher costs
under a microsecond, and walking frames costs about sixteen. A dispatcher
that walks frames only for host-facing answers, the only ones the
tolerance rules judge, and does a prefix test and a counter for the rest,
spends:

- about 2 ms a port at a dozen contexts' worth of calls, 2,000;
- about 1.6 ms a port for 100 host-facing answers with their frames, the
  inventory's unique count per worker.

That is under 1% of a survey. The 10% budget is comfortable, provided
frames are kept for host-facing answers only; phase 1 measures it
directly.

## What phase 1 has to respect

From reading the evaluator:

- **Parity with today's filters.** Shadow mode matches the baseline only
  if the judgment it feeds `observe.Tolerate` is today's: the
  Portfile-frame condition, `glob` always flagged, the `file` subcommand
  list, the symlink rule, and a missing file under the tree root counted
  as captured. The dispatcher can record more, and the wider ledger rides
  alongside for the later phases; phase 1 changes what is recorded, not
  what is judged.
- **What `interp hide` can't reach.** It refuses namespace-qualified
  names. `::env` is a variable and needs a read trace, as the inventory
  used. `registry::` isn't in the 2.12 worker; its `registry_*` parent
  aliases are. Base's aliases to the parent (`get_tool_path`,
  `get_compiler_version`, `getoption`, `findBinary`, `sysctl`, and the
  rest) do their host I/O in the parent, so they are re-aliased at the
  worker boundary.
- **One ledger per worker.** Workers are one per `mportopen` and subport,
  native sessions are reused, and up to eight interpreters run at once.
- **Base's own loading.** Hiding `source`, `file`, and `glob` routes
  Base's own port1.0 loading through the dispatcher. Today's tracing
  starts at `PortSystem`, so this is new traffic, and it is what the
  cost figures above bound.
- **Phase 3 needs a callback.** Materializing a read on demand needs Tcl
  to call Go, and the RPC today is Go→Tcl only.
- **The evaluator's environment.** The evaluator inherits dockhand's
  whole environment, `HOME` included, and uses the host's `portdbpath`.
  `portindex` already writes a private `macports.conf` through `PORTSRC`,
  which is the precedent for phase 6.

## The five decisions, with their data

The scope left five decisions for this session. What the data says, and
what is recommended; the decisions are the person's.

1. **Toolchain questions in modelled contexts, before the facts table.**
   In modelled contexts Base itself asks the host toolchain questions in
   about 22,000 ports:
   - whether `/usr/lib/libxcselect.dylib` exists, 22,626;
   - `get_tool_path clang`, 22,115;
   - `get_compiler_version`, 22,071;
   - whether the CLT's `make` is executable, 22,047;
   - `xcodeversion`, 18,502.

   On a Mac these are already answered by the host, silently, since
   `ModelVariables` applies only off a Mac. Refusing them would make half
   the tree inconclusive until phase 5. *Recommended:* let the host
   answer, tagged `host-in-model` in the ledger, flagged but not refused.
   Phases 1–4 lose no coverage, and the tag counts what phase 5 must
   replace.
2. **Phase 4's behaviour change.** Evaluation reads the host's installed
   ports today:
   - registry queries in about 950 ports, 836 of them through
     `qt5_version_info`;
   - prefix files read by Portfile or PortGroup code in about 900, such
     as qt4's 399 and openssl's 80;
   - Base's own look for an installed MacPorts clang in 4,409 ports'
     modelled contexts.

   So a plan made on this Mac can differ from one CI or a Tart guest
   would make from an empty prefix. *Recommended:* yes, `update` on a Mac
   plans against an empty prefix, as CI and Tart see it, whatever is
   installed. It is what "evaluation independent of host state" means.
3. **How far "inconclusive" blocks.** *Recommended:* as the prospective's
   §4 has it, only the edits that depend on the unresolved field. The
   Java PortGroup's `java_home` (183 ports, read through
   `exec /usr/libexec/java_home` in a callback) then blocks a Java port's
   checksums and fetch edits until phase 5 answers it, but not its
   revbump.
4. **Performance.** The measurements above. *Recommended:* keep the
   budget, at most 10% over untraced for shadow mode, with the design
   rule that frames are captured only for host-facing answers. The
   estimate is under 1%. The untraced reference is this session's
   baseline run.
5. **Proving phases without a person at the Mac.** The repository is
   public, so GitHub's macOS runners cost nothing. *Recommended:* a
   sampled survey in CI, about a thousand ports stratified over the
   outcome buckets and the 674 host readers, with MacPorts Base from its
   official installer, compared against a sample baseline kept in the
   repository. Whole-tree surveys stay on the Mac, one per phase. It is
   not needed before phase 1.
