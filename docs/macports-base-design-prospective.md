# MacPorts Base interface: prospective design

Status: a prospective design, revised 2026-09-24 after an
[independent review](reviews/2026-09-24-macports-base-independent-review.md)
and an executable baseline against Base master. Written during the walk
through the [contracts review](reviews/2026-09-23-contracts-review.md)
([direction record](reviews/2026-09-23-contracts-direction.md), decision
39). Nothing here is implemented. What remains to decide is at the end.

## Why

Dockhand evaluates Portfiles by running MacPorts' own `port-tclsh` with
Tcl it ships, because MacPorts is the authority on what a Portfile means.
Everything the evaluator knows comes from Base: the fetch plan checksums
are computed from, the declarations an edit is placed by, the hooks the
fetch guard judges, the platform contexts preparation models, and, once
the oracle of decisions 8 and 16 exists, every question evaluation asks
the host.

Dockhand's policy is to depend only on interfaces an external component
officially exposes (decision 32). Base is the deliberate exception
(decision 39). `mportinit`, `mportopen`, and the related primitives are an
external application API by Base's own `doc/INTERNALS`, and dockhand's use
of them is not the problem. The exposure comes from what dockhand needs
beyond them: declaration origins, hook inspection, a fetch plan without
fetching, and control over what evaluation observes. The `port` client
cannot provide those, and reimplementing Portfiles in Go would be a far
larger liability. Base's lead developer, Joshua Root (@jmroot), drives
Base through `port-tclsh` the same way and keeps his tools working across
Base versions with version-conditional code. Dockhand does the same,
openly, and guards every assumption so a Base change disables the
operation that depends on it rather than letting dockhand evaluate
wrongly.

The target is the current release, 2.12.6, with the differences Base
master already shows handled and tested.

## What dockhand touches today

A read-only survey on 2026-09-23 compared dockhand's Tcl
(`internal/macports/eval/*.tcl`, `internal/verify/tart/guest.tcl`) with
how MacPorts' own people and tools use Base (scratch in
`/tmp/tclsh-survey/`). Classes: **A**, used by @jmroot's gists, by mpbb
(the build scripts MacPorts' CI and buildbots run), or by
macports-contrib; **B**, the API the `port` client uses; **C**, dockhand's
alone.

| Touch point | Class | Used for | Alternative | Stability, 2.8 to master |
|---|---|---|---|---|
| `package require macports`, `mportinit`, `mportopen file://…` with subport and variants, `mportinfo`, `mportclose` | A, B | Evaluate a Portfile | — | Stable |
| `ditem_key $h workername`, `$worker eval` | A | Reach the port's interpreter | — | Stable |
| `macports::os_*`, `build_arch`, `macports::version` | A, B | Platform and version | — | Stable |
| `vercmp` | B | Version ordering | — | Master loads Pextlib only in `mportinit` |
| `option`, `exists` in the worker | Equivalent to A's `_mportkey` | Read options | `_mportkey` | Stable |
| `macports::override_vars` | A (mpbb `mirror-multi.tcl`, Base's `portindex -p`) | Model another platform | — | Since 2.11.0; removes variable traces (`macports.tcl:2066`) |
| Writing `::macports::sources*`, `porturl_prefix_map` after `mportinit` | C | Make the tree the source | A: `PORTSRC` (mpbb's pattern) | `porturl_prefix_map` since 2.10.0 |
| `trace add execution ::macports::worker_init leave`, `PortSystem leave` | C | Run code in each port interpreter | None | Stable |
| `rename ::file` in the worker | C | The Linux model's Command Line Tools | Partly A: `tool_path_cache`, `macosx_sdk_*` overrides (mpbb `index_vars`) | Master moves compiler checks to the parent |
| Declaration and host-access traces with `info frame` | C | Declarations, host access | None | Master's parent-side helpers escape them |
| A copy of livecheck's default-type logic | C | Offline livecheck | None | Moved to `portlivecheck_run.tcl`'s `_livecheck_main` on master |
| Target and hook records through `ditem_key`; the `user<proc-…>` wrapper and body prefix | C | Fetch details, hook bodies | None | Stable |
| The definition walk (`namespace which`, `interp alias`, `info procs/args/body`), keyed by spelling (`fetchguard/effect.go:21`) | C | Ship hook callees to the fetch guard | Verified descriptions of the helpers the guard needs | Many helpers become parent aliases on master |
| `portfetch::checkfiles`, `urlmap`, `assemble_url`, `ports_fetch_no-mirrors` | C, close to A | Native fetch plan | `mportexec … distfiles`; the option through `mportopen` | Lazily loaded on master |
| `::macports::fetch_credentials`, copies of `curlwrap`'s credential logic | C | Credential applicability, offline | None | Changes often; the probe adapts and fails closed |
| `PortInfo(portgroups)`, `portpath`, `worksrcpath` | B | Hook origins | `mportinfo` | Stable |

Go also depends on Base's representation directly: it compares the
fetch target's procedure with `portfetch::fetch_main`
(`fetchguard/grammar.go:26`) and strips Base's literal hook prefix
(`:413`).

References: @jmroot's gists
[`mportkey.tcl`](https://gist.github.com/jmroot/175892576ff9ecae1cff79e7701cf63c),
[`has_archive.tcl`](https://gist.github.com/jmroot/e6fb957b7051e0ecbdd3565d6a269b79),
[`all_bin_available.tcl`](https://gist.github.com/jmroot/f524dcfe5fdadcd8b7a9c2e46151e0d4),
[`deps_diff.tcl`](https://gist.github.com/jmroot/f7ec18f982b3a1fed1f823f78b9affe0),
[`all_distributable.tcl`](https://gist.github.com/jmroot/f84c329919356bfb1ed2d8425f3cfebb);
[mpbb](https://github.com/macports/mpbb) `tools/mirror-multi.tcl`,
`index_vars/macosx_*`, `tools/dependencies.tcl`,
`tools/sort-with-subports.tcl`, and commit `02cfeb1` ("Support future
base versions").

## Evidence

### The executable baseline, 2026-09-24

Dockhand's MacPorts packages were run against the installed 2.12.6 and a
local build of master (`/opt/macports-master`, 2.12.99 at `0c70cb739`,
Tcl 9.0.4) in a scratch copy. The patches, comparison output, runtime
probes, and test logs are kept in
`~/.dockhand/surveys/2026-09-24-base-master-baseline/`.

- **2.12.6:** all 19 packages pass (429 tests, 2 skipped), 16.7 s.
- **Master, unpatched:** 61 tests fail, all at the startup check's
  `::vercmp`, which master loads only in `mportinit` (e545ebe8c,
  `macports.tcl:1145`).
- **Master with four small patches passes everything** (21.5 s):
  1. the command check before `mportinit` and the `vercmp` ordering
     check after it;
  2. `portutil::target_load` for the fetch target before `fetch_main` and
     `checkfiles`, as Base's own `fetch_async_start` does
     (`portfetch.tcl:14`, `:142`; `portutil.tcl:975`), about 11.5 ms per
     port interpreter against under 1 ms on 2.12.6;
  3. Tcl 9's `glob` returns an empty list where 8.6 raised an error, which
     the livecheck copy relied on; `glob -nocomplain` with an explicit
     error on an empty list behaves the same on both (Base's own guard at
     `portlivecheck_run.tcl:101` has the same Tcl 9 problem upstream);
  4. the livecheck fingerprint test reading `portlivecheck_run.tcl`.
- **Real Portfiles agree.** 659 Portfiles (2,570 ports and subports,
  9,499 distfiles: every Portfile with a `pre-fetch` hook, PortGroup
  samples, 250 at random, plus deno, wasmer on Darwin 22, a python stub,
  and a Java port) give identical fetch plans, fetch-guard verdicts (540
  guarded, 91 custom, 1,939 standard), option errors, and livecheck
  values on both Bases.
- **The serious difference fails no test.** Master runs compiler and SDK
  probes in the parent `portlib` package (`portlib.tcl:347`, `:542`,
  `:810`), where the worker's traces cannot see them. Eleven ports that
  read the host now look host-independent, natively and in modeled
  contexts: qt4-mac, qt64-qtwebengine and its docs, poedit, godot,
  openjdk8, gcc48, gcc49, gcc8, libgcc8, gpsd. The adapter must account
  for the aliased `portconfigure::*` and `portlib::*` entry points at the
  worker boundary.
- **The fetch guard narrows safely.** A hook calling `quotemeta` or
  `getdisttag` is guarded on 2.12.6 and refused on master, since those are
  parent aliases now (`macports.tcl:2246`, `:2256`); no `pre-fetch` hook
  in the tree calls one directly.
- **Tcl 9 and the RPC layer.** Its tests pass on both, but a non-UTF-8
  call argument or an unencodable reply character ends a Tcl 9 session
  (`encoding convertfrom` outside a `catch` in `loop.tcl`). Also on Tcl 9:
  a non-UTF-8 Portfile is refused (none exist today), `file normalize`
  no longer expands `~`, `expr 010` is 10.
- **An intentional Base change.** Master derives extract dependencies
  from `extract.only`'s suffixes (14fdd90f9, 5c05e1eff): about 20 ports
  gain or lose `bin:xz:xz`/`bin:unzip:unzip`. Golden outputs are per Base
  version for this reason.

### What the review's probes showed

Master's `portlib` caches file existence (`portlib.tcl:335`), SDK roots
(`:345`), clang compilers (`:624`), and mirror URLs keyed by mirror file,
not platform (`:1150`). Reseeding the file cache left a previous
environment's SDK in place; a mirror URL built from `${os.major}` kept
Darwin 22's value in a Darwin 25 context until the mirror cache was
cleared. That second case lands in a fetch input.

### Configuration isolation

- `PORTSRC` is appended after the installation's and the user's
  `macports.conf` (`macports.tcl:1233`): an overlay. Any key it does not
  restate survives from them.
- **HOME set to an empty directory** hides `~/.macports/macports.conf` on
  both Bases. Unsetting HOME breaks `mportinit` on both: the fallback
  through `dscl` is misquoted (`macports.tcl:1212` on master, `:1135` on
  2.12.6).
- **A private `portdbpath`** gives an empty registry on both (0 installed
  images against 423 by default; `registry_active zlib` not active) and
  moves `distpath`, `ccache_dir`, and the image mode with it. Its
  `registry/` directory must exist first. But Base persists host facts
  under `${portdbpath}/cache`: `xcodeinfo`, `macos_version`,
  `compiler_versions`, `pingtimes` (`macports.tcl:642` `load_cache`,
  `:662` `save_cache`, `:695`, `:1426`, `:2042`, `:7388`). A persistent
  directory therefore carries facts from one process to the next: the
  follow-up review ran two master processes against one cache directory,
  and the second returned a compiler version the first had saved, for a
  compiler that does not exist. A fresh directory costs about 27 ms per
  `mportinit`.
- **The host's 2.12.6 with these knobs** gave the same metadata as the
  default configuration across the 659 Portfiles on this host. That is
  evidence of compatibility, not of isolation: only the tests that
  deliberately change host inputs, below, can establish isolation.
- **Still leaking** unless restated or modeled: `archive_sites_conf`,
  `pubkeys_conf`, `archivefetch_pubkeys`, `startupitem_install`,
  `macportsuser`; compiled-in `install.user`/`group` and tool paths; the
  environment `mportinit` keeps (HOME, JAVA_HOME, PATH, `*_SITE_LOCAL`,
  proxies, DYLD_*, DISPLAY) and reads (SUDO_USER, SSH_AUTH_SOCK,
  CCACHE_DIR); Xcode and Command Line Tools detection from the host.

### A private runtime

Base installed in one place can evaluate as another prefix. With
`PORTSRC` setting `prefix /opt/local`, `applications_dir`,
`frameworks_dir`, `portdbpath`, `sources_conf`, and `variants_conf`, a
Base at `/opt/macports-master` evaluates `${prefix}` as `/opt/local` and
writes nothing under `/opt/local`. Caveats: `mportinit` requires
`${prefix}/share/macports` to exist (`macports.tcl:1599–1601`), so the
modeled prefix must be present on the host; `${prefix}/bin` is put on
PATH and prefix-relative probes read the real `/opt/local`
(cyrus5-imapd globs `/opt/local/include/db*`), which the closed
installation of decision 16 must answer; compiled-in paths (install user
and group, tool paths) come from the build, so a runtime must be built
with a clean PATH. The local master build compiled in
`/opt/macports-test/bin` tool paths because that directory came first on
PATH.

### Startup cost

`port-tclsh` with `package require macports` and `mportinit`, warm: 34.5
ms on 2.12.6, 37.7 ms on master; isolation adds nothing measurable with a
persistent private `portdbpath`.

## Design

### Principles

1. **An environment is a process invariant.** A process is started for
   one concrete environment and retired when it changes; there is no
   platform reset. Base's caches (files, SDK roots, compilers, mirrors)
   and `override_vars`' removal of traces make in-process switching
   unsound, and startup costs about 35 ms. `Evaluator.Observe` already
   starts a fresh session per call; that property is kept. The first
   installation pass and the pass with the dependency closure active
   (decision 16) are different environments, and so are the assignments
   explored when an answer is unknown. A pool may later reuse processes
   within one environment, keyed by Base identity, resource tree,
   platform and toolchain facts, configuration, installation model, and
   oracle policy, once equivalence tests justify it.
2. **Three responsibilities stay separate**, even as a few Tcl files in
   one package: the **Base adapter** knows where an operation lives in a
   given Base and how to invoke or intercept it; the **environment
   policy** knows which facts and effects are permitted and records each
   answer's origin; the **preparation policy** decides whether the
   observations justify an edit. The environment policy is written once,
   not per Base family.
3. **The adapter returns semantic records, not Base's representation.**
   Fetch target classification (standard, custom, unknown), the hook's
   own body and source origin, declaration observations, the fetch plan,
   and resolved command identities. Go keeps the judgment of effects and
   stops comparing Base procedure names or stripping Base's hook prefix.
4. **Failure is proportional to the missing evidence.** Capabilities are
   separate: metadata reading, declaration tracing, fetch-plan
   inspection, hook interpretation, offline livecheck, controlled
   evaluation. Each preparation path requires its declared capabilities
   and no others: a revision bump or an explicit version needs no
   livecheck, while missing evidence the selected path does use blocks it.
   A livecheck fingerprint
   mismatch does not stop metadata inspection; a failure of controlled
   evaluation stops opening Portfiles. The existing
   `TestUnknownFetchLayoutKeepsMetadataButBlocksArchivePreparation` is
   the precedent.
5. **Every environmental question has a defined answer.** A cache seed is
   an optimization, never the boundary. A query that reaches no model
   goes to the environment policy, which answers, explores, or records it
   as unresolved; it never silently inherits the host. This covers the
   parent aliases and resource-loading interpreters, not only the worker.
6. **An unresolved observation survives `catch`.** Portfiles and Base
   wrap probes in `catch`; a refused program whose error is caught must
   still leave its mark. A ledger outside the Portfile's control flow
   records unresolved observations, and any entry makes the context
   inconclusive whatever `mportopen` returned. A fully modeled program
   failure (a known exit status) is an answer, not an unresolved entry.

### The environment contract

Evaluation initializes under a configuration dockhand owns, not only the
eventual `mportopen`:

- `PORTSRC` restating every key that affects evaluation: `prefix`,
  `applications_dir`, `frameworks_dir`, `portdbpath`, `sources_conf`,
  `variants_conf`, `archive_sites_conf`, `pubkeys_conf`,
  `startupitem_install`, `macportsuser`, the build and universal
  architecture keys, and whatever an audit of each supported Base's
  configuration keys adds;
- `HOME` pointed at an empty directory dockhand owns, never unset;
- a disposable writable `portdbpath` per process, made from a prepared
  template with an empty `registry/` and no `cache/`, so the registry
  stays empty, the closed installation of decision 16 answers registry
  questions, and no cached fact outlives its environment; the environment
  policy says which cache entries may be read and which writes are
  allowed. A persistent directory per full environment identity is a
  later optimization, once tests across process restarts (the canary
  included) and across Base revisions justify it;
- host detection that runs during bootstrap (`mportinit`'s `sw_vers`,
  `sysctl`, `xcodebuild` and `xcode-select`, `dscl`) replaced or tracked
  before its results can affect evaluation;
- the process launched with Base's kept environment variables only, at a
  builder's values (decision 16), and nothing else from the person's
  shell;
- the modeled prefix kept distinct from where the runtime lives;
- credentials an authorized downloader uses kept out of evaluation's
  provenance: applicability and policy are recorded, never secrets.

Preparation evidence records the evaluator's identity (Base version,
source commit, and build configuration or artifact identity, since
compiled-in paths come from the build), the adapter revision, the
facts and policy identity, and the effective environment.

### The adapter interface

| `dh::` | 2.12 (`base212`) | Master preview |
|---|---|---|
| `init` | `mportinit` under the environment contract | Same; command checks before init, `vercmp` after |
| `open`, `close`, `info`, `option` | `mportopen` with `ports_fetch_no-mirrors`, `mportinfo`, `_mportkey` form | Same |
| `model_platform` | `override_vars` with mpbb's `index_vars` keys, including `tool_path_cache` and `macosx_sdk_*`, once per process | Same |
| `fetch_plan` | `checkfiles`, `urlmap`, `assemble_url`, Base's `master_sites` fallback for an unmapped tag; returned as records | `portutil::target_load` for the fetch target first |
| `fetch_target` | standard, custom, or unknown, from the target record | Same |
| `hooks` | each hook's own body and source origin | Same |
| `definitions` | Portfile and PortGroup procedures, with resolved identities (interpreter, namespace, bound arguments), not spellings | Same, plus verified descriptions of the aliased Base helpers the guard needs; unknown callees stay unsupported |
| `livecheck` | the copied logic, `glob -nocomplain` with an explicit empty check, fingerprinted against Base's procedure | fingerprinted against `_livecheck_main` |
| `host_interfaces` | worker Tcl I/O, Pextlib commands, parent aliases | plus the `portconfigure::*`/`portlib::*` entry points the worker reaches, which is where master's compiler and SDK probes happen |
| `credentials_probe` | as today, adaptive and failing closed | Same (verified on Tcl 9) |
| `capabilities` | the capabilities the probes and canary established | Same |

The guest keeps building in its real environment through `port`;
preparation's modeled answers never replace facts during verification.
It shares only the small inspection subset it needs. For local Tart, the
evaluator's Base and the guest's pinned Base should match by default; if
they differ, verification records both.

### Bootstrap and choosing an adapter

The lifecycle of an evaluator process: `package require macports`; read
`macports::version`, which both Bases provide before `mportinit` (2.12.6
and 2.12.99 verified; master's `vercmp` is not loaded yet); choose the
adapter; write the environment contract (the `PORTSRC` file, `HOME`, the
disposable `portdbpath`); `mportinit`; apply the once-only model
overrides; run the probes and the canary; only then open Portfiles.

`macports::version` chooses a family for released versions: 2.12.x is
`base212`. Master is never chosen by version text,
since many different builds report 2.12.99: the preview adapter is
selected explicitly for development (`DOCKHAND_BASE_ADAPTER`), is
identified by the Base source commit together with its build
configuration or artifact identity, and never bypasses the checks. 2.12.99 can never match `base212`. An entirely
unknown family declines evaluation, while offline status and recorded
evidence stay usable, and the message says which operations are
unavailable and why, rather than telling everyone to upgrade.

### Guards

- **Checks before initialization and after it, separately:** commands
  that must exist before `mportinit`; `vercmp` ordering and the rest after
  it.
- **Structural probes per process**, mapped to capabilities:
  `worker_init`'s arguments, `override_vars` names resolving and reading
  back, `PortSystem`, the option aliases, the fetch helpers after
  `target_load` where it exists, whole-phase override naming, the
  livecheck fingerprint, `curlwrap`'s arguments and `::uri::split`.
- **A behavioral canary per environment, in its own process.** A small,
  offline Portfile embedded in dockhand, with its resources pinned so a
  ports-tree change is not mistaken for a Base failure. Its expected
  values (fetch plan, a declaration's frame, a hook record, livecheck, an
  option read, and for modeled environments `use_xcode` and the compiler)
  come from its own declared fixture and profile, not a single
  expectation for every profile.
- The guarantee is stated as tested behavior plus explicit checks of
  known assumptions, never as detection of every future change.

### Testing

- Pinned Base revisions for repeatable integration tests: the current
  release and a pinned master preview, run on every adapter change.
- A scheduled macOS CI job on floating master, keeping the commit, the
  diagnostics, and normalized differences; local runs for investigation.
- The integration suite covers: fresh against reused processes and
  A/B/A context ordering, including both installation passes and
  distinct unknown-outcome assignments; changed host inputs with
  unchanged modeled results (configuration, registry, compilers,
  environment, filesystem layout); parent aliases, callbacks, code before
  `PortSystem`, and resource loading outside the port interpreter;
  refusals caught by Tcl code leaving unresolved evidence; tagged
  distfiles and patches, the missing-tag fallback, custom targets, and
  declaration provenance through the adapter's own frames; the RPC layer
  and list decoding under Tcl 9, including invalid UTF-8 in both
  directions; and selected real Portfiles under both Bases compared with
  ordinary MacPorts under the same controlled inputs.
- Golden outputs are per Base version; cross-version differences are
  reviewed, never accepted automatically, since upstream changes can be
  intentional (master's extract dependencies are).
- Tests take the Base under test from the opt-in variable, never from
  PATH. Today some fall back to PATH lookup, and on a machine whose PATH
  starts with another MacPorts installation they silently test that one.

### A private runtime

Isolation may not need one: on this host, the host's 2.12.6 under the
environment contract gave the same results as a private install, which
the tests that change host inputs must still confirm. A private runtime's
value is the Base version itself. Dockhand would evaluate with the Base it was
tested against and the guest builds with, rather than whatever the
person's `selfupdate` last installed, and the refusal when the host's Base
is newer than dockhand supports would no longer arise. It is unmodified
Base, installed by dockhand, built with a clean PATH, configured through
the environment contract. Costs: installation and update tooling, the
Command Line Tools to build it, a Linux build for the Linux model, a
prompt update when a Base release ships since ports soon rely on new
features, and the modeled prefix's `share/macports` still has to exist on
the host. It is prototyped after the adapter boundary exists, and made
the default only if the prototype earns it. The existing
`Evaluator.Executable` and `Prefix` selection is most of the seam.

### Order of work

1. The startup fix and its regression test, **together with a version
   gate**: command checks before `mportinit`, `vercmp` after, and
   preparation and modeled evaluation allowed only on validated released
   families (2.12.x, and 2.11.x while it stays cheap; done 2026-09-24,
   with `DOCKHAND_TEST_BASE_ADAPTER=preview` admitting master to the
   evaluator's tests only). Today the only thing stopping Base master from
   preparing is the startup check failing by accident; fixing it without
   a gate would let master prepare with the eleven known host-access false
   negatives. On 2.12.99 and unknown versions preparation is refused, or
   affected observations are conservatively inconclusive, until the
   parent-side evidence of step 5 exists.
2. The small compatibility fixes the baseline found, each with a test
   that fails on master today: `target_load` before the fetch helpers
   where it exists; the livecheck `glob`; the RPC layer's encoding
   conversions caught; tests taking Base only from the opt-in variable.
   With them, regressions for the parent-side gap: a synthetic port
   whose fetch input depends on a parent-side compiler or SDK query, and
   the eleven real ports from the baseline.
3. Environment ownership: one process per environment, the environment
   contract, and the normalized record contract, with the representation
   dependencies moved out of Go. No change to preparation policy.
4. The adapter boundary: shared Base mechanics, with explicit version
   differences only where evidence requires them.
5. The controlled-environment dispatcher (the oracle) incrementally, with
   the unresolved-observation ledger, starting with the toolchain and
   installation queries the inventory found, and master's parent-side
   compiler and SDK entry points.
6. Validation against real environments and Portfiles before the
   host-independence claims are made; the private-runtime prototype
   alongside.

## Remaining decisions

1. **Support policy.** The current release plus a pinned master preview
   as the required matrix; 2.11.6 kept only while the shared
   implementation makes it cheap.
2. **The canary**, per environment and per process, with fixture-declared
   expectations: yes.
3. **The preview selection**: development only, explicit, never bypassing
   checks, identified by commit or fingerprint.
4. **Scheduled master checks** on macOS CI, with local runs for
   investigation.
5. **The private runtime's priority**: prototyped after the adapter
   boundary (step 4), rather than first.

Adopted from the review's follow-up (2026-09-24), which approved the
revised architecture: persistent storage belongs to the environment
(the disposable `portdbpath`); preview preparation stays disabled until
the parent-side host-access evidence is restored (the step 1 gate); the
bootstrap order; per-path capabilities; runtime identity including the
build; and the isolation claims qualified as evidence of compatibility
until the host-changing tests pass.
