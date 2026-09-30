# Helper ownership and shared concepts review

Reviewed 2026-09-30 through `fe03fa1ab62931e71cb27bda98bca94b055d69e4` (`feat(macports): a port's variants, and a target known by its variants`). Work began at `d2ad9f77`; the variant commit was included and the relevant tests rerun against it. Source references below are to `fe03fa1a`. Uncommitted variant-workflow changes were excluded.

This is a fresh scan of helper ownership, repeated domain interpretations, and missing concepts, building on the [previous review](2026-09-28-private-helper-ownership.md) and [follow-up](2026-09-28-private-helper-follow-up.md). It is not a comprehensive correctness audit.

## Assessment

The recent extractions are useful. Planning has a pure decision layer; eligibility belongs to MacPorts; Portfile inspection shares setup-command knowledge with editing; binary-archive signing no longer belongs to SSH. The old-manifest completeness gap is fixed. The structural remedies for nine of the original ten helper findings are now present; the three SSH readiness loops remain the explicitly scheduled exception.

The next concentration is **interpreting upstream source and deciding what it means for a port**. Reading a project's manifests is spread across creation, dependency generation, and comparison. Comparison policy is then split between `sourcecompare` and private engine helpers. Those boundaries deserve attention before adding more manifest and workspace support.

Two focused package boundaries would help: a shared project/manifest reader, and a MacPorts-specific update assessment. Other findings below need small operations in existing packages, not more layers. P2 denotes a reproduced behavioral discrepancy; P3 denotes an organizational improvement or a contract issue whose production effect is not established.

The size inventory still puts engine at 11,677 production Go lines and command at 8,478, despite planning's extraction. Portedit has 3,725; sourcecompare has 969. These count each package's own files, without tests or subpackages. Growth alone is not a reason to split them; the concrete responsibilities below are.

## Findings

### 1. P2 Manifest helpers flatten facts that their newer consumers need

**Evidence:** `internal/sourcecompare/manifests.go:21`, `:68`, `:117`, `:165`, `:191`, `:211`; `internal/sourcecompare/compare.go:49`, `:395`; `internal/engine/update.go:741–789`.

The private manifest readers still reduce dependencies to `map[string]string`. That was sufficient for some summaries, but the newer Python pin checker makes decisions from those summaries. The requirement regex stops at `;`, discarding environment markers. `Requirement` then carries only name and specifier. A repeated name overwrites its earlier declaration, even if the declarations apply under different conditions. The policy layer cannot recover the lost distinctions.

Small probes reproduced both directions of the Python problem:

- Changing `requests>=2; sys_platform == 'win32'` to the same requirement for `'darwin'` produces no findings: applicability changed, but the stored name and constraint did not.
- Moving a Windows-only pin from `requests==1` to `requests==999`, with MacPorts' `py313-requests` at version 1, produces a hold against a Darwin preparation. `pythonPins` receives no marker with which to recognize that the requirement is inactive there.

Cargo has the same representational pressure. `cargoRequirement` returns a table's `version` before inspecting its Git source or revision. A changed Git revision alongside an unchanged `version = '1'` produces no dependency-change finding. That is missing information, not a request to reverse D9's decision that Go/Rust dependency changes do not hold submission.

**Recommended concept:** a requirement record retaining ecosystem, name, constraint, source, extras, condition, and declaration scope where applicable. Preserve multiple conditional declarations and incomplete readings. Unknown applicability should remain unknown instead of silently becoming unconditional. Comparison can summarize these records; pin assessment can inspect them before applying a constraint.

This also supplies a practical shared-reader boundary. `newport.Declare` and `newport.Binaries` independently decode Cargo.toml (`internal/macports/newport/manifest.go:25`, `:62`); comparison decodes it again; `dependency.GoRequirement`, `GoBinary`, and `generateGo` each parse go.mod (`internal/macports/dependency/go.go:25`, `:36`, `:51`), while comparison has another projection. Repeated parsing alone is not a defect, but ownership of manifest facts is now scattered across three consumers. `GoBinary` in particular is project-output knowledge in a dependency-generation package.

A small `internal/project` package could own typed manifest readings and shared build-system vocabulary. Start with the formats already shared, preserving raw fields and parse/inheritance limitations. Use ecosystem-specific records rather than one universal manifest schema. Keep creation's detection preference, category choice, PortGroup generation, and destroot guesses in `newport`; keep MacPorts dependency-block policy in `macports/dependency`; keep comparison policy outside the reader. The current inferred binaries are marked unconfirmed, which is appropriate: a package/module name alone should not become proof that a binary exists.

### 2. P2 The HTTPS probe has become a second owner of redirect policy

**Evidence:** `internal/engine/https.go:47–68`; `internal/fetch/fetch.go:64–102`; `internal/engine/create.go:174–180`.

`requestProbe.Answers` uses `http.DefaultClient.Do`, follows redirects, and returns true for the final status below 400. It never checks whether the response still came over HTTPS. The existing `fetch.Open` explicitly rejects HTTPS-to-HTTP redirects. Thus two pieces of network code give opposite answers about the same redirect, and `create` can describe/write an HTTPS replacement whose successful response is actually plain HTTP.

A local in-memory transport returning HTTPS 302 → HTTP 200 reproduced this: `requestProbe` returns true while `fetch.Open` rejects the redirect. No external site was contacted. This establishes the disagreement, not a claim about any particular real homepage.

**Recommended ownership:** keep the engine's decision to inspect URLs and the existing create/update policies. Put the transport operation in `fetch`, sharing its redirect checks and accepting an HTTP client. Return a small probe result with outcome and final URL, retaining failure information if the caller needs to explain it. Do not simply call `Open` unchanged: its exact-200 download contract differs from a HEAD/range probe that can legitimately receive other successful statuses. Share the transport rules while keeping those success criteria explicit.

### 3. P2 Portfile traversal still has two meanings of a script body

**Evidence:** `internal/macports/portfile/inspect.go:21–52`; `internal/macports/portfile/version.go:90–107`; `internal/macports/portfile/variants.go:21–47`; `internal/tcl/syntax/visit.go:13–35`.

The new private `bodies`/`commands` helpers know which words are executable Tcl/MacPorts bodies. Version-candidate discovery repeats that knowledge. Archive-variant discovery instead calls `Script.Commands` with an always-true descent predicate, which descends into every braced word that parses as a script, including data.

Two probes demonstrate the difference:

```tcl
set documentation {variant imaginary {distfiles-append example.tar.gz}}
variant docs description {Documentation} {set example {checksums sha256 aaaa}}
```

`ArchiveVariants` returns `imaginary` for the first and `docs` for the second. Neither example executes an archive declaration. Its callers in `portedit/artifact_plan.go:191` and `portedit/checksums.go:168`, `:233` use these names to request additional variant observations. The false discoveries are reproduced; extra evaluation or a refusal downstream is an inferred consequence, not an exercised full update failure.

**Recommended ownership:** give `portfile` one traversal operation that distinguishes script bodies from data and carries scope information. Inspection, archive-variant discovery, and candidate discovery can choose their own descent policy over it. Keep Tcl control syntax in `tcl/syntax`; MacPorts' `variant`, `subport`, and `platform` conventions belong in `portfile`. A generic all-braces walker is still useful for lexical searches, but cannot be treated as evidence that a declaration executes.

There is a smaller repetition alongside it: `inspect.subportBody` and `BumpRevision` independently locate a single literal subport block. That can become a shared private operation in the same package. No additional package is needed for either change.

### 4. P3 Typed option access needs one shared error and presence contract

**Evidence:** `internal/macports/variants.go:25–53`, added by the latest commit; `internal/macports/metadata.go:69`; `internal/macports/facts.go:39–45`; `internal/macports/eligibility.go:64–87`.

The new `PortInfo.Variants` is in the right package and has a useful record: defaults, requirements, conflicts, and description. However, each typed accessor still implements its own policy for reading `Options` alongside `OptionErrors`. `Bool`, `PortGroups`, and build eligibility consult the failure map; `Variants` does not. It also discards parsing errors for nested `requires` and `conflicts` lists.

Probes show an explicit evaluation error for either `variants` or `vinfo` returning a successful empty list, and malformed nested constraints returning a variant without those constraints. At this commit the new accessor has test callers but no production callers. This is a contract gap to close before item 8 consumes it, not a demonstrated failure of the current check command.

**Recommended concept:** small checked option readers in `macports`, distinguishing value, absence, and evaluation failure, with list/dictionary decoding that retains parse errors. Domain accessors decide what an absent option means. Reuse these mechanics inside the package; export them only for actual external consumers. Do not migrate the entire options map or introduce a schema framework to solve this.

### 5. P3 Upstream update assessment is accumulating as private engine policy

**Evidence:** `internal/engine/update.go:626–809`; `internal/sourcecompare/compare.go:124–130`, `:310–340`, `:395–452`; `internal/model`'s `UpstreamChange`; `internal/command/json_results.go:86–118`.

`compareUpstream` now handles pair coverage, archive comparison, build-system relevance, hold suppression, dependency counting, deduplication, and extraction of Python requirements. `pythonPins` adds package-to-port matching and constraint checks; `toolchainChange` adds another kind of assessment. Meanwhile `sourcecompare.proven` already applies the policy that builds prove some dependency changes. This is a coherent domain operation spread across a low-level comparison package and an approximately 180-line tail of engine helpers.

The return shape shows the missing concept: `(*UpstreamComparison, []pythonRequirement)` carries the decision and an engine-private side channel to finish it. Conversion to `model.UpstreamChange` retains kind/path/message/hold but discards build-system and requirement information. Some aggregation keys are English messages. That makes future workspace/context support harder to compose and inspect, even where today's result is correct.

**Recommended boundary:** a focused `internal/macports/updatecheck` operation, or equivalent narrowly named package, taking source observations, the selected port's evaluated build facts, and dependency-version observations. It should return an assessment with structured findings, applicability, reasons, and incomplete work. Engine retains Git/source selection, I/O orchestration, and persistence. Preserve the existing D9/D12 behavior; this is an ownership recommendation, not a policy change.

Avoid passing the entire `Engine` or `preparation.Result` into the new package. A small explicit input keeps the boundary useful. Preserve structured facts through assessment; historical stored prose need not be migrated merely to improve the internal flow. This extraction and the project reader in finding 1 are complementary: one reads what upstream declares, the other decides what that means for this port.

## Smaller opportunities and boundaries to retain

| Area | Useful next step |
| --- | --- |
| Baseline classification | `engine.rebuildWhere` (`baseline.go:171`) and `BaselineWorthy` (`:236`) repeat the install/test/advisory-failure predicate. Give that result classification one local owner; keep selection and wording separate. No new baseline package is needed for this duplication. |
| Final prepared metadata | `engine.describe` and `preparedPort` (`update.go:407`, `:435`) interpret fidelity history and `Unchanged`, while comparison reads `Prepared` directly. An editor-result accessor for the selected final port would keep consumers from knowing these representation details. No inconsistent production result was reproduced. |
| Creation category validation | `engine/create.go:181` checks category text locally, while `macports.IsCategory` owns the exclusion of dot/underscore directories. Define a complete category-input validator in MacPorts and use it before writing a port; the top-level directory classifier alone is not a complete input validator. |
| Build-system vocabulary | `newport.Build.System`, `sourcecompare.systems`, and `macports.BuildSystem` describe related facts. Share identity/file classification with the project reader, but preserve creation's ordered detection and its `autoreconf` recipe distinction. MacPorts' PortGroup-to-system mapping remains MacPorts' own. |

Keep the useful new boundaries: `planning` decides from observations without reading the repository; `binaryarchive` owns signatures/site entries while Tart transports them; `preparation` embeds the editor result instead of copying its fields. The roadmap deliberately declined an unused evaluation-report layer and eager engine-dependency assembly; this scan supplies no reason to reopen those choices. SSH readiness, submission phases, and environment wording are already scheduled work, not new discoveries here. Do not combine source archives, built archives, and installable signed entries into a generic archive service.

## Suggested order and validation

First fix the data-loss and redirect discrepancies in their present owners, with the focused regressions. Before wiring the new variant accessor into planning, make its failure contract explicit. Consolidate Portfile body traversal while fixing archive-variant discovery. Then extract the shared manifest readings and update assessment as workspace/manifest coverage expands. These can be separate changes with stable behavior, rather than another broad rewrite.

At `fe03fa1a`, the existing suites passed for `planning`, `model`, `sourcecompare`, `macports`, `macports/portfile`, `macports/newport`, `macports/dependency`, and `macports/binaryarchive`. Focused engine tests passed for archive comparison, Python pins, build relevance, HTTP notices, and import boundaries. The native evaluator's new variant-reporting test passed using `/opt/local/bin/port-tclsh`. This was not a full repository test run; opt-in dependency-helper live tests were not enabled.

The [reproduction patch](2026-09-30-helper-ownership-probes.patch) contains seven temporary test functions covering the discrepancies above. All seven failed their intended assertions in an isolated export, including both archive-variant data examples and the option-error/nested-list cases. The HTTPS test uses an in-memory transport, not a network server. Probes are review artifacts, not changes to the checkout's application or test code. Apply the patch to an isolated checkout of the reviewed commit and run:

```sh
go test ./internal/sourcecompare ./internal/engine ./internal/macports ./internal/macports/portfile -run '^TestHelperReview' -count=1
```

Only this report, the probe patch, and an activity note were added to the working repository. No application code or roadmap was changed.
