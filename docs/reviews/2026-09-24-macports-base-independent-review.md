# Independent review of the MacPorts Base proposal

2026-09-24. A second opinion for the refactoring discussion, not an accepted
design or an implementation. Read alongside
[the prospective design](../macports-base-design-prospective.md) and
[the direction record](2026-09-23-contracts-direction.md), especially
decisions 8, 16, 17, and 39.

**Recommendation:** keep MacPorts as the evaluator, introduce the adapter,
and preserve the Go/Tcl process boundary. Before implementing the proposed
interface, make each modeled environment immutable for the lifetime of a
process, separate Base mechanics from environment policy, and define
capabilities in terms of the evidence an operation needs. Consider a
Dockhand-managed, pinned installation of unmodified Base as a separate
deployment option.

The immediate master compatibility work looks tractable. The larger risk
is the promise of host-independent evaluation: an adapter can organize the
coupling without actually making that promise true.

**What I checked.** Dockhand at `3108db74`, including the uncommitted design
documents; the local Base checkout at
`0c70cb73999b48619167c296fc5cb87ef2201647`; the installed 2.12.6 runtime;
and the already available `/opt/macports-master` runtime, reporting Base
2.12.99 and Tcl 9.0.4. The installed master's `macports.tcl` and
`portlib.tcl` match those in that checkout byte for byte. I also read the
local mpbb survey checkout and upstream source. No implementation files or
existing design documents were changed for this review.

| Check | Result |
| --- | --- |
| Existing `internal/macports/eval` suite on `/opt/local/bin/port-tclsh` | Passed, 3.848 seconds reported by Go. |
| Existing runtime-inspection test on master | Failed at the predicted pre-init `vercmp` check. |
| Direct master startup probe | `vercmp` absent before `mportinit`, present afterwards. |
| Inert synthetic Portfile on master | `fetch_main` and `checkfiles` absent after open; both present after `target_load`; fetch plan obtained without fetching. |
| SDK context-switch probe on master | Reseeding only the file-existence cache still returned the previous environment's SDK. |
| Synthetic platform-dependent mirror on master | Switching Darwin 22 to 25 still returned the Darwin 22 URL until the mirror cache was cleared. |

These are bounded checks, not certification of master compatibility or of
the proposed oracle. The two context-switch probes exercise Base directly;
they do not demonstrate a current Dockhand production failure.

**1. Keep the native evaluator, and distinguish its public API from the
extra instrumentation.**

Using `port-tclsh` is not itself the architectural mistake. Base describes
`mportinit`, `mportopen`, and related primitives as an external application
API in `doc/INTERNALS`. Dockhand's exposure comes from the additional
requirements: observing declaration origins, inspecting hooks, obtaining a
fetch plan without fetching, and controlling environmental observations.

Replacing that work with `port` output parsing would give up useful
semantics and introduce a different coupling. Reimplementing Portfiles in
Go would be a much larger liability. I agree with both rejections.

I would preserve the existing package boundaries around source editing,
fetch judgment, and evaluation. This needs a focused extraction inside
the evaluator, plus a few changes to the data it exports; it does not need
a new plugin framework or parallel implementations of the whole evaluator.

**2. Make the environment a process invariant.**

I would remove `reset_platform` from the normal adapter contract. Start a
process for a concrete environment and retire it when that environment
changes. Initially use fresh processes for individual observations; reuse
them only within a fixed context after equivalence tests justify it.

The proposed reset of `portlib::configure::file_exists_cache` is
insufficient. The source also has an SDK cache and caches for compiler and
mirror data. Some answers depend on platform variables not included in
their cache keys. `override_vars` also removes variable traces, so
restoring values alone is not a general restoration of initialization.

The direct SDK probe used two synthetic developer directories, both with
an explicitly modeled SDK present. It produced:

```text
context_A=/tmp/dockhand-review-tools-A/Platforms/MacOSX.platform/Developer/SDKs/MacOSX26.0.sdk
context_B_after_reseeding_file_cache=/tmp/dockhand-review-tools-A/Platforms/MacOSX.platform/Developer/SDKs/MacOSX26.0.sdk
context_B_after_clearing_sdk_cache=/tmp/dockhand-review-tools-B/Platforms/MacOSX.platform/Developer/SDKs/MacOSX26.0.sdk
```

The mirror probe used a temporary resource whose `review` mirror URL was
`https://example.invalid/${os.major}/`. No network request was made:

```text
darwin22=https://example.invalid/22/
darwin25_after_clearing_file_cache=https://example.invalid/22/
darwin25_after_clearing_mirror_cache=https://example.invalid/25/
```

That second example demonstrates the possible consequence directly in a
fetch input. Clearing the additional caches would repair these examples;
it would also commit Dockhand to discovering every future cache and its
dependencies. Process lifetime is a simpler ownership rule.

Today's `Evaluator.Observe` already starts a fresh session for each call
through `evaluate`. Preserve that useful property. A future pool can be
keyed by Base identity, resource tree, platform/toolchain facts,
configuration, installation model, and oracle policy. A change from the
empty installation to the modeled dependency closure is a change of
environment too. Unknown-outcome exploration must not share caches across
different assignments.

Process isolation prevents cross-context memory contamination. It does
not, by itself, stop reads of the real host or contain filesystem effects.

**3. Cache seeding is an optimization; it cannot be the oracle boundary.**

An unseeded `file_exists` query still falls through to the real filesystem.
In addition, `choose_compiler` uses `file executable` directly, and SDK
selection has `glob` and `exec` paths. A worker-only replacement of `file`
does not cover those parent-side operations.

The oracle needs a defined answer for every supported environmental
query, including queries made by parent aliases and resource-loading
interpreters. A cache miss must reach that policy, which can answer,
explore, or report unknown; it must not silently inherit the host.

Keep three responsibilities distinct, even if they begin as a few Tcl
files in one package:

- The Base adapter knows where an operation lives and how to intercept or
  invoke it for a particular implementation.
- The environment policy knows which facts and effects are permitted and
  records the origin of each answer.
- The preparation policy decides whether the observations justify an edit.

This avoids duplicating the oracle policy in `base212` and the preview
adapter. Share mechanisms where the contracts are actually the same, and
use explicit adapter differences where evidence requires them.

A refusal must also survive Tcl error handling. Portfiles and Base
legitimately use `catch` around probes. If an unknown program is denied,
then its error is caught and evaluation produces a plausible fallback,
the result must still carry an unresolved observation. Keep an evaluation
ledger outside the Portfile's normal control flow; do not infer
completeness from a successful `mportopen` alone. A fully modeled program
failure is different: its exit status can be a known answer.

For the direction record's safety goal, define the effect boundary
separately from this semantic ledger. Worker command hiding is useful, but
parent aliases and native extensions also need accounting. If a stronger
containment guarantee is wanted, test an OS-enforced boundary around the
whole process. A requirement to resist malicious Portfiles would be a
separate scope; this review does not assume it.

**4. `PORTSRC` improves source selection but does not isolate configuration.**

I agree with replacing the three source-variable writes with the normal
configuration path. However, `mportinit` appends `PORTSRC` to a list of
configuration files after the installation and user configuration files.
It is an overlay, not an instruction to ignore the others.

A temporary file containing only `sources_conf` therefore does not
establish decision 16's host independence. The environment contract must
cover inherited configuration and defaults, variants, user configuration,
environment variables, registry location, compiler preferences, and
toolchain settings. Initialization must happen under that contract, not
only the eventual `mportopen`.

Distinguish the location of runtime files from the prefix being modeled.
Installing an evaluator under a private directory should not accidentally
make Portfiles describe a builder with that private installation prefix.
Keep credentials used by an explicitly authorized downloader out of
evaluation provenance; record applicability and policy, never secrets.

**5. Make the adapter return semantic records, not Base's representation.**

The proposed `fetch_plan`, observation, and livecheck operations are good
boundaries. The raw hook and definition records need more normalization.
Currently Go still compares the target procedure with
`portfetch::fetch_main` and strips the literal Base-generated hook prefix
in `internal/macports/fetchguard/grammar.go`. Merely moving Tcl calls
behind `dh::` would leave these representation dependencies outside it.

Have the adapter report a standard/custom/unknown target classification,
the hook's user body and source origin, declaration observations, and
resolved command identities. Keep the independent effect judgment in Go.
The adapter owns the evidence that a particular Base procedure or wrapper
has that meaning.

I would not make a generic walk into every parent procedure a prerequisite
for master support. An alias is not just a body: the target interpreter,
namespace, bound arguments, and access to variables matter. Today's
definition map keys by command spelling; two equal short names in
different namespaces are not necessarily the same command.

Prefer a small set of verified semantic descriptions for Base helpers the
guard actually needs, while continuing to inspect Portfile/PortGroup
procedures conservatively. Unknown callees can remain unsupported. If the
definition walk is expanded, give it explicit identities and bindings
before treating parent bodies as equivalent to worker procedures.

The guest can share the small Base-inspection subset it needs. It should
continue to build in its actual environment through `port`; preparation's
modeled answers should not replace facts during verification.

**6. Keep failure proportional to the missing evidence.**

Version families are useful for choosing an implementation and stating
support. They are not proof that every semantic dependency is unchanged.
Nor do structural probes and a canary guarantee detection of every future
incompatibility. Describe the guarantee as tested behavior plus explicit
checks of known assumptions.

Separate at least metadata reading, declaration tracing, fetch-plan
inspection, hook interpretation, offline livecheck resolution, and
controlled-environment evaluation. Automatic preparation must require all
the capabilities its path needs. A livecheck fingerprint mismatch need
not disable independent metadata inspection; a containment failure may
prevent opening any Portfile at all. Existing
`TestUnknownFetchLayoutKeepsMetadataButBlocksArchivePreparation` is a good
precedent.

For an entirely unknown Base family, declining evaluation is reasonable.
Offline status and already recorded evidence should remain usable. A
known family's optional capability failure should explain the operation
it disables rather than tell everyone to upgrade Base, especially when
the new Base is what introduced the incompatibility.

The preview identifier should avoid predicting the next release number.
Record the actual source commit or an installation fingerprint: many
different master builds report 2.12.99. Do not let a generic 2.12.x match
accidentally select the released-family adapter for that preview.

**7. Consider owning the evaluator runtime's lifecycle.**

Adapters address source compatibility. A private, pinned installation of
unmodified Base addresses the separate problem of a user's `selfupdate`
changing Dockhand's evaluator underneath it.

This is the alternative I would investigate alongside the adapter:
Dockhand selects a tested runtime explicitly, while use of the host's
installation remains available for development and comparison. The
existing `Evaluator.Executable`/`Prefix` selection already provides much
of the seam. This does not require a permanent Base fork or a rewritten
Portfile interpreter.

There is a real cost: installation and update tooling, Tcl/native-library
dependencies, platform builds, runtime identity, and keeping the pinned
Base compatible with the selected ports tree. Pinning is a release
discipline, not a way to freeze Base indefinitely. It also does not solve
host modeling or containment by itself.

I would make this deployment option possible now and decide whether it
should become the default after a small prototype. It reduces the need
to support arbitrary user installations more effectively than adding
ever more version guards.

Record evaluator identity, adapter revision, facts/policy identity, and
the effective environment with preparation evidence. Master version text
alone is insufficient. Verification should record its own identity. For
local Tart, matching the evaluator and guest Base is the simplest default;
if they differ, report that and consider a guest-side check of the
prepared fetch inputs. A successful build on one platform does not prove
every modeled preparation context.

**8. Test the claimed semantics, then the implementation shapes.**

The behavioral canary is worth having. Keep it small, offline, and scoped
to the selected environment and requested capabilities. The expected
`use_xcode` and compiler values must come from its declared fixture/profile;
a CLT-only expectation is not appropriate for every Xcode profile.

A native canary run before modeling can itself populate parent caches.
Run different environments in separate processes. Pin any canary resources
whose semantics are meant to be invariant; otherwise a ports-tree change
could be mislabeled as a Base failure.

The broader integration suite should cover:

- Fresh versus reused execution and A/B/A context ordering, including the
  dependency-installation passes and distinct unknown-outcome assignments.
- Changed host inputs with unchanged modeled results: configuration,
  registry contents, compiler availability, environment, and filesystem
  layout.
- Parent aliases, callbacks, code before `PortSystem`, and resource loading
  outside the main Portfile worker.
- Denials caught by Tcl code, which must still leave unresolved evidence.
- Tagged distfiles and patches, missing-tag fallback, custom targets, and
  declaration provenance through the adapter's added call frames.
- The existing RPC, list decoding, and hook analysis under Tcl 9, beyond
  adapting a single credentials command.
- Selected real Portfiles under both Base families, compared against
  ordinary MacPorts under the same controlled inputs. Differences should
  be reviewed, not automatically accepted into golden files.

Do not demand that different Base releases produce identical answers;
upstream semantic changes may be intentional. Compare each adapter with
its own Base first, then inspect cross-version differences.

Use pinned release and preview revisions for repeatable integration tests.
A scheduled CI job can separately test floating master and preserve the
commit, diagnostics, and normalized differences. For the current refactor,
run the pinned preview tests on adapter changes as well as on the schedule.

**Suggested sequence.**

1. Fix the startup ordering and add the regression test independently.
   Separate checks possible before initialization from those requiring it.
2. Establish an executable baseline on installed 2.12.6 and a pinned master:
   explicitly load target code where needed and characterize the remaining
   differences before freezing the adapter interface.
3. Specify immutable environment/process ownership and the normalized
   observation contract. Extract shared Base mechanics and narrow version
   differences without changing preparation policy.
4. Introduce the controlled-environment dispatcher incrementally, with a
   persistent unresolved-observation ledger. Start with toolchain and
   installation queries already identified by the inventory.
5. Validate against real environments and real Portfiles, then enable the
   stronger host-independence claims. Evaluate the private-runtime
   deployment option during this work.

This changes the prospective ordering in one important respect: settle
environment ownership before building an adapter with platform-reset
semantics. Build the oracle incrementally after that boundary is clear.

For the four open questions: current stable plus a pinned preview is a
reasonable required matrix; keep 2.11.6 only if the shared implementation
and existing tests make its support cheap. Yes to the canary with the
qualifications above. Yes to an explicit development-only preview
selection, which must not bypass checks. Prefer repeatable macOS CI for
the scheduled master check, with local runs for investigation.

**Source and execution references.**

- Dockhand: `internal/macports/eval/evaluator.go`, `evaluator.tcl`,
  `observation.go`, `observation.tcl`, `compatibility.tcl`, and
  `compatibility_test.go`; `internal/macports/fetchguard/effect.go` and
  `grammar.go`; `internal/verify/tart/config.go`.
- Base at the reviewed commit:
  [parent helpers and caches](https://github.com/macports/macports-base/blob/0c70cb73999b48619167c296fc5cb87ef2201647/src/portlib1.0/portlib.tcl),
  [initialization, configuration, overrides, and worker aliases](https://github.com/macports/macports-base/blob/0c70cb73999b48619167c296fc5cb87ef2201647/src/macports1.0/macports.tcl),
  [target loading](https://github.com/macports/macports-base/blob/0c70cb73999b48619167c296fc5cb87ef2201647/src/port1.0/portutil.tcl).
- Relevant source positions in that checkout: `portlib.tcl:335` file
  cache, `:345` SDK cache, `:542` direct executability check, `:624`
  compiler cache, `:1150` mirror caches; `macports.tcl:1233`
  configuration layering, `:2066` overrides; `portutil.tcl:975`
  target loading.
- Test commands (the temporary Go cache only avoids writing to the normal
  user cache):

  ```sh
  GOCACHE=/tmp/dockhand-codex-review-go-cache \
  DOCKHAND_TEST_MACPORTS_TCLSH=/opt/local/bin/port-tclsh \
    go test ./internal/macports/eval -count=1

  GOCACHE=/tmp/dockhand-codex-review-go-cache \
  DOCKHAND_TEST_MACPORTS_TCLSH=/opt/macports-master/bin/port-tclsh \
    go test ./internal/macports/eval \
      -run '^TestRuntimeInspectionAndVersionEvidence$' -count=1 -v
  ```

  The second command is expected to fail on the reviewed Dockhand code.

---

**Follow-up review, 2026-09-24, after Claude's revision and executable
baseline.**

I would proceed with the revised architecture. It resolves the main
concerns above: immutable environments per process, a shared environment
policy, semantic adapter records, unresolved observations surviving
`catch`, and capability-specific failures. Deferring the private-runtime
prototype until the adapter boundary exists is reasonable. The earlier
sections describe the first proposal; this addendum assesses the revision.

I inspected the saved test events and comparison data under
`~/.dockhand/surveys/2026-09-24-base-master-baseline/`. The reference and
final patched-master logs each contain 429 passing tests and two skips,
with 19 passing packages. The unpatched-master log contains 61 failing
tests. The two native sweep files each contain 659 successful cases and
2,570 ports/subports. Comparing their records confirms 9,499 distfiles,
identical fetch plans and guard assessments, and matching livecheck
options. Option errors agree after normalizing the Base version label in
their messages. The eleven `HostAccess: true -> false` changes named in
the revised design are present. These are checks of the saved artifacts;
I did not rerun the full corpus.

Two acceptance conditions should be explicit before preview preparation
is enabled:

1. **Persistent storage belongs to the environment too.** The environment
   contract at design lines 254–256 introduces a persistent private
   `portdbpath`. Base also reads and writes `${portdbpath}/cache`, including
   compiler and Xcode information. A fresh process therefore does not
   necessarily start from fresh facts.

   I independently ran two master processes against a temporary cache
   directory. The first saved a synthetic compiler version through
   `macports::save_cache`. In the second, `get_compiler_version` returned
   that value from disk for a compiler path that does not exist:

   ```text
   process A: saved_model_A_compiler=111.0
   fresh process B: fresh_process_compiler=111.0
   fresh process B: compiler_actually_exists=0
   ```

   The relevant source is `macports.tcl:642` (`load_cache`), `:662`
   (`save_cache`), and `:7383` (deferred compiler-cache loading). The
   completed oracle could prevent this through deterministic overrides;
   make that responsibility explicit. Scope persistent state to the full
   environment identity, or use disposable writable state with a prepared
   empty-registry template. Define which caches may be read, which writes
   are allowed, and how the registry remains empty. Test across process
   restarts, including the canary, and across Base revisions. The measured
   startup saving is a reason to optimize this after correctness is clear.

2. **Restore host-access evidence before making master preparation usable.**
   Steps 1–2 remove the startup and fetch barriers; the current evaluator
   has no version gate. The parent-side observation work is scheduled in
   step 5. If intermediate changes are shipped or used for preparation,
   passing the existing suite could therefore enable the eleven known
   false negatives. Keep preview preparation disabled, or conservatively
   inconclusive for affected observations, until the missing evidence is
   restored. Add focused synthetic regressions that make a fetch input
   depend on a parent-side compiler/SDK query, plus representative real
   ports from the baseline. This does not require finishing every oracle
   capability first.

Three smaller contract clarifications would help implementation:

- **Bootstrap order.** The adapter's `init` promises controlled
  initialization, but adapter selection is described as occurring after
  `mportinit`. Specify the lifecycle: identify Base and select the
  implementation, establish initialization policy, call `mportinit`,
  apply the once-only model overrides, validate, then open Portfiles.
  `macports::version` is available after `package require macports`,
  before `vercmp` is loaded. Any host detection admitted during bootstrap
  must be replaced or tracked before its results can affect evaluation.
- **Required capabilities.** Replace “automatic preparation needs all of
  them” with “each preparation path requires its declared capabilities.”
  Revision-only preparation or an explicit-version path should not acquire
  an unrelated livecheck requirement. Missing evidence that the selected
  path actually uses must still block it.
- **Runtime identity.** A source commit identifies source, not the complete
  installation. The baseline itself found compiled-in tool paths from a
  different prefix. Include build configuration/artifact identity as well
  as the source revision when identifying a private or preview runtime.

The comparison between default and isolated evaluation is useful evidence
of compatibility on the tested host. The planned tests that deliberately
change host inputs are what can establish the stronger isolation claim.
This distinction should also qualify the assertions at design lines
160–162 and 349–350.
