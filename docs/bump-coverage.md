# Core bump coverage

Status: target-workflow prerequisites and all four stages have implementations as of 2026-09-16. Support remains bounded by the contracts below; [validation](activity/2026-09-16-bump-coverage-validation.md) distinguishes successful preparation from unresolved cases and unrun builds. This continues the [evaluator-driven planner](bump-planner.md). The immediate objective is to prepare more real port updates without weakening preservation of independent releases or publication evidence.

## Evidence and scope

The existing Terraform/Helm release-series, gh source/binary, and Deno architecture-archive exercises already passed complete archive preparation. Keep them as regression controls. Their remaining support should not be described as entirely new work.

The [readiness investigation](activity/2026-09-16-core-bump-readiness.md) reproduced Wasmer's reported failure and checked sixteen selected controls. The earlier 147-Portfile survey remains a comparison corpus, not a whole-tree success estimate. Shared-subport cases require explicit candidate checks: finding a version input alone does not test whether sibling edits are acceptable.

The section numbers below identify capabilities, not the current queue. Follow the [roadmap](roadmap.md) for remaining work. Stages 2, 4, and 3 were implemented in that order. The architecture and preservation requirements below remain the contract for extensions.

## Prerequisite: target resolution and contribution continuity

The [target workflow plan](target-workflow.md) supersedes the earlier name-resolution-only prerequisite. This prerequisite is implemented: source-bound name resolution replaces public `--subport`; verify/publish continue the selected contribution; contribution identity precedes preparation; outcomes distinguish standalone builds from updates; and native HTTP livecheck supports automatic Terraform discovery.

The intended ordinary commands are `dockhand bump terraform-1.16`, `dockhand verify terraform-1.16`, and `dockhand publish terraform-1.16`. An explicit version remains available; `--branch` is an override, not a required handoff. Internal subport identity and exact source/evidence binding remain necessary. The plan defines manual checkout selection, retry and ambiguity behavior, package placement, and migration/concurrency tests.

Target continuity and stage 1 below completed the first milestone; stages 2–4 extend that coverage. Named target selection does not itself authorize shared-release sibling edits, and successful explicit-version archive preparation is not proof of automatic release discovery.

## 1. Distinguish fetch guards from archive modifications

Implemented; retained below as the contract for this capability. See the [integrated exercise](activity/2026-09-16-target-workflow-validation.md) for Wasmer preparation and verification results.

Wasmer 7.4.0 has a `pre-fetch` hook on Darwin < 23 arm64 that reports an error and returns. The earlier generic fetch-hook check rejected Darwin 22 arm64 on that source; the same source archive is observable on all six inspected Darwin 22/23/25 and arm64/x86_64 combinations. There is no evidence here that Wasmer builds on the rejected platform.

The adapter now distinguishes standard archive semantics, recognized rejection-only guards, and unsupported/custom behavior in a structured fetch assessment. It preserves procedure/hook facts and a diagnostic reason. Hook bodies stay inside `macports/eval`; higher layers consume facts rather than parsing Tcl strings or error messages.

The implemented recognition covers the general shape of Wasmer's already-registered unconditional diagnostic/error hook, independent of port name. Accept only the known MacPorts scope wrapper, permitted diagnostic statements, safe message substitutions, and an unconditional error return. Do not execute fetch hooks to discover whether they mutate something. Unknown commands, command substitutions in messages, array-index substitutions, writes, URL/distfile mutations, replaced fetch procedures, and post-fetch work remain unsupported. Preserve the existing narrowly checked Go toolchain guard.

Archive editability and platform availability are separate facts. `known_fail` alone is not a license to discard a context or bypass a hook. A recognized guard may permit archive preparation while still reporting that the platform cannot fetch/build normally. Do not remove the guard, claim build coverage, or suppress failure if the user actually verifies that platform.

Initially retain the existing archive/checksum coverage requirements even for guarded contexts. Wasmer can reuse the shared archive facts; a unique uncovered artifact or changed pin must still block the plan. This avoids silently leaving an old checksum in a branch whose source URL changed.

Implemented acceptance cases:

- Wasmer's reported 7.4.0 -> 7.4.1 preparation passes this guard check, preserves the guard and its OS/architecture condition, and reports the rejected context accurately.
- A supported-platform Tart verification runs normal MacPorts phases. Any later dependency-regeneration, patch, or build failure remains a separate finding, not a promised success from fixing this guard.
- Mutating, arbitrary, and mixed hooks stay rejected, including mutations before an eventual error and executable substitutions in diagnostics.
- Aseprite's post-fetch Git work remains unsupported. Go guard behavior and independent version/checksum pins remain covered by regressions.
- `assess`, explicit-version assessment, and actual preparation use the same classification and explain which hook/context prevents progress.

Local candidate/context planning now precedes dependency-source downloads and helper execution. Application reuses that source-bound plan without repeating the probes; the old-manifest/helper comparison remains required before accepting regenerated dependency edits. Regressions establish that a locally unsupported candidate triggers neither a download nor helper execution. Missing helpers retain their actionable errors.

This stage fits the current single-target workflow and needs no new CLI flag, state migration, or production dependency.

## 2. Improve platform observations and context selection

Implemented through source-bound scalar writes and option results in `eval`, with profile selection in `portedit`. Native operand observations retain source frames without repeating getters. The source scanner remains a backstop for unvisited branches. Supported comparisons include inverted forms; aliases and formatting use bounded Darwin-major metadata enumeration. Mutable, external, or unresolved thresholds and unmodeled minor-version/deployment dimensions remain explicit limitations. See the [implementation report](activity/2026-09-16-platform-observations.md).

py-openssl's named subport, libfec, and mpir reach local input assessment. mrustc's option threshold resolves, but its host-state access still prevents a complete coverage claim.

Keep context selection policy in `portedit`; strengthen the evidence supplied by `eval`. Record executed platform reads and relevant condition operands with their source frames and captured PortGroup provenance. Use actual Tcl evaluation to observe values. A value observed on one host is not proof that a variable is a constant on every profile.

Begin with source-bound scalar/option thresholds whose values can be checked across baseline and candidate observations. Track unresolved reads and newly discovered boundaries explicitly. Repeat observations when they introduce another supported boundary, with a bounded budget that ends in an incomplete-coverage result rather than presumed success. Reads in formatting/build arguments must not be silently ignored merely because their command is usually unrelated to fetching; they need a defensible classification, or remain an explicit limitation.

Retain the source scanner as a conservative backstop for unvisited branches. If runtime observations plus supported syntax cannot establish the needed contexts, report uncertainty. Observing several profiles is not proof of arbitrary Tcl coverage. Model the declared OS/architecture dimensions only; variants, SDKs, filesystem state, and arbitrary external programs do not become modeled by implication. Compare context requirements discovered before and after the candidate edit.

Acceptance cases include a literal threshold, a scalar/option threshold, inverted comparisons, an alias used in a conditional, a formatting-only read, and a candidate that activates a different source branch. Independent old-OS releases must remain byte-for-byte unchanged at their editable version/revision/checksum declarations. Include counterexamples with mutable thresholds, external reads, unsupported platform dimensions, and unresolved conditions. Measure interpreter/session counts and timing on the same controls, including beets, before expanding the modeled matrix.

## 3. Represent a shared release across subports end to end

This broader capability covers a release shared by several subports in one Portfile, such as py-memprof or py-ipdb. Those ports expose an editable input; candidates require explicit shared-release authorization. Terraform release-series siblings and libusb-devel demonstrate why an entire Portfile must not automatically become one release scope.

Implemented as `record.ReleaseScope`, derived by the editor and persisted on the revision (schema 17): the requested target, the exact related targets affected by the selected declaration/source, and protected targets/contexts. Membership requires declaration provenance plus before/after source evidence; matching version strings, names, or merely living in one file are insufficient. Auxiliary metaports with no archive/build still need recorded metadata changes and preservation checks.

Use `assess <subport> --shared-release --version <version>` or `bump <subport> <version> --shared-release --diff` to inspect the cohort. `bump --shared-release` authorizes it. Without the flag, sibling changes remain a refusal. The initiating buildable subport remains the handle for verify/publish/status. A metadata-only parent is recorded but is not itself a supported initiating archive target. This is not a generic multi-port batch interface.

The editor and preparation result now carry the scope separately from the initiating target. Candidate checkpoints and immutable revisions retain it. Correction/adoption/reassociation revalidate the owning Portfile; persistence prevents membership from being dropped. Evidence reuse and publication check all required members. Related buildable members must share archive identity and checksum owners; identical version strings alone are insufficient. No-archive parents are excluded from builds only when native evaluation confirms no configuration, an empty build, and no build hooks.

Reuse existing per-target verification plans and isolated attempts where their contracts fit. Shared-release targets are not reverse dependents: do not set `IncludeDependents` as a shortcut or run dependency discovery to manufacture their scope. Keep scope independent of verification selection, then require an explicit coverage policy for each affected target. Initially, use isolated Tart attempts for each buildable target; do not imply a shared source archive proves every subport builds. Unsupported provider coverage must produce an actionable result rather than falling back to publishing on a single passing attempt.

Acceptance requires parent/shared-subport preparation, preservation of pinned siblings, unique revision/checksum ownership, one stored source tree, durable target scope, conflicting-target isolation, cancellation/restart, corrective edits, and a publication test proving that one missing or failing required target blocks publication. This stage must not ship by deleting the current sibling-fidelity checks.

## 4. Identify dependency source archives independently of auxiliary files

Implemented: extraction/worksrcdir facts select candidate files, and archive contents establish exactly one manifest owner. Independent, unextracted auxiliary archives may remain alongside the source without being downloaded or regenerated. Native `extract.rename` determines whether a differing top-level directory is acceptable. The real Codex control passed this selection and exposed a separate Cargo `rev=` mapping limitation, since resolved by following the port's offline policy; see the [manifest report](activity/2026-09-16-manifest-sources.md) and the [Git reference report](activity/2026-09-16-cargo-git-references.md).

Keep artifact identity/ownership in `distfiles`, manifest/helper validation in `dependency`, and orchestration in `portedit`. Match the selected upstream source, extraction/worksrcdir facts, and archive contents to establish which artifact provides the manifest. Do not choose the first archive or infer ownership only from a filename. Reuse the existing complete artifact plan and preserve independently pinned auxiliary URLs, declarations, and checksums.

This was implemented independently of shared-subport publication. Preserve baseline helper-output comparisons and maintained overrides. Missing cargo2port/go2port must still fail with the existing actionable prerequisite message.

## Package and dependency decisions

The preceding target-resolution work introduces the focused `macports/selection` boundary. No additional package or library is required for the fetch-guard increment. `macports/eval` already provides the needed boundary around native Tcl. Add focused files for fetch semantics/diagnostics and later platform observations there; do not move interpretation into `workflow` or add per-port implementations.

Keep release scope, candidate validation, context selection, and edit authorization in `macports/portedit`; `distfiles` continues to bind artifacts to exact checksum declarations. Shared observation types belong in `macports`, and only durable contribution/verification facts belong in `record`. A later package split needs a concrete independent consumer or dependency boundary, not a file-count threshold.

Continue evaluator-guided candidate generation and full forward validation. Multiple independent version inputs, new release-series creation, and coordinated Rust/bootstrap maintenance remain separate work. This pass does not introduce a generic Tcl inverse or proof engine.

## Validation and secondary findings

Use authored behavioral fixtures for positive and refusal cases, then replay pinned real Portfiles in disposable snapshots. Run the full 147-case comparison at the end of a behavior-changing milestone, and report local input discovery, candidate checks, actual archive preparation, and builds separately. Use the public-archive exercises for Terraform, Helm, gh, and Deno as regression controls, preserving their historical source versions where necessary.

The older sample also contains seventeen findings reported as missing SHA256 or ambiguous groups. Before claiming those as coverage wins, distinguish weak/absent digests from ownership ambiguity. A later checksum-upgrade feature should compute and add a strong digest while preserving correct ownership; removing the SHA256 requirement alone is not the fix.

Wasmer already passed preparation and supported-platform verification in the previous milestone. The shared-target pass adds durable restart, isolated-attempt, cancellation, and publication-gate integration exercises, plus real historical archive preparation for py-ipdb and py-memprof. Those historical interpreter cohorts were not built or published as live PRs; do not treat preparation success as build compatibility. A future suitable current shared-release candidate should exercise live Tart-to-PR behavior.
