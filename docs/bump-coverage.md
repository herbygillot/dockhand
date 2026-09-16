# Next pass: core bump coverage

Status: target-workflow prerequisites and stage 1 are implemented on 2026-09-16; stages 2–4 remain planned. This continues the [evaluator-driven planner](bump-planner.md). The immediate objective is to prepare more real port updates without weakening preservation of independent releases or publication evidence.

## Evidence and scope

The existing Terraform/Helm release-series, gh source/binary, and Deno architecture-archive exercises already passed complete archive preparation. Keep them as regression controls. Their remaining support should not be described as entirely new work.

The [readiness investigation](activity/2026-09-16-core-bump-readiness.md) reproduced Wasmer's reported failure and checked sixteen selected controls. The earlier 147-Portfile survey remains a comparison corpus, not a whole-tree success estimate. Shared-subport cases require explicit candidate checks: finding a version input alone does not test whether sibling edits are acceptable.

The section numbers below identify capabilities, not the current queue. Follow the [roadmap](roadmap.md): finish contribution lifecycle and routine cleanup, then platform observations (stage 2), manifest/auxiliary archive separation (stage 4), and shared-release scope (stage 3). Each implementation should complete through assessment, preparation, and applicable verification/publication checks before broadening its claims.

## Prerequisite: target resolution and contribution continuity

The [target workflow plan](target-workflow.md) supersedes the earlier name-resolution-only prerequisite. This prerequisite is implemented: source-bound name resolution replaces public `--subport`; verify/publish continue the selected contribution; contribution identity precedes preparation; outcomes distinguish standalone builds from updates; and native HTTP livecheck supports automatic Terraform discovery.

The intended ordinary commands are `dockhand bump terraform-1.16`, `dockhand verify terraform-1.16`, and `dockhand publish terraform-1.16`. An explicit version remains available; `--branch` is an override, not a required handoff. Internal subport identity and exact source/evidence binding remain necessary. The plan defines manual checkout selection, retry and ambiguity behavior, package placement, and migration/concurrency tests.

Target continuity and stage 1 below completed the same milestone; stages 2–4 extend coverage in the order maintained by the roadmap. Named target selection does not itself authorize shared-release sibling edits, and successful explicit-version archive preparation is not proof of automatic release discovery.

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

`portedit/profiles.go` currently treats every textual Darwin-major read as a literal numeric comparison. That detects some missing contexts conservatively, but rejects harmless build-triplet formatting in libfec/mpir and cannot resolve py-openssl's PortGroup-supplied minimum or mrustc's option-based threshold. Adding more text patterns indefinitely is not the intended architecture.

Keep context selection policy in `portedit`; strengthen the evidence supplied by `eval`. Record executed platform reads and relevant condition operands with their source frames and captured PortGroup provenance. Use actual Tcl evaluation to observe values. A value observed on one host is not proof that a variable is a constant on every profile.

Begin with source-bound scalar/option thresholds whose values can be checked across baseline and candidate observations. Track unresolved reads and newly discovered boundaries explicitly. Repeat observations when they introduce another supported boundary, with a bounded budget that ends in an incomplete-coverage result rather than presumed success. Reads in formatting/build arguments must not be silently ignored merely because their command is usually unrelated to fetching; they need a defensible classification, or remain an explicit limitation.

Retain the source scanner as a conservative backstop for unvisited branches. If runtime observations plus supported syntax cannot establish the needed contexts, report uncertainty. Observing several profiles is not proof of arbitrary Tcl coverage. Model the declared OS/architecture dimensions only; variants, SDKs, filesystem state, and arbitrary external programs do not become modeled by implication. Compare context requirements discovered before and after the candidate edit.

Acceptance cases include a literal threshold, a scalar/option threshold, inverted comparisons, an alias used in a conditional, a formatting-only read, and a candidate that activates a different source branch. Independent old-OS releases must remain byte-for-byte unchanged at their editable version/revision/checksum declarations. Include counterexamples with mutable thresholds, external reads, unsupported platform dimensions, and unresolved conditions. Measure interpreter/session counts and timing on the same controls, including beets, before expanding the modeled matrix.

## 3. Represent a shared release across subports end to end

This broader capability covers a release shared by several subports in one Portfile, such as py-memprof or py-ipdb. Those ports already expose an editable input, but an actual candidate correctly fails today's single-target fidelity policy. Terraform release-series siblings and libusb-devel demonstrate why an entire Portfile must not automatically become one release scope.

Introduce an explicit release scope in the editor's plan: the requested target, the exact related targets affected by the selected declaration/source, and protected targets/contexts. Membership requires declaration provenance plus before/after source evidence; matching version strings, names, or merely living in one file are insufficient. Auxiliary metaports with no archive/build still need recorded metadata changes and preservation checks.

Recommended user experience: assessment/preview explains the shared release and lists the targets that must move together. An explicitly selected subport must not silently enlarge the contribution. Decide the smallest explicit CLI expression for accepting that scope when implementing this stage; do not introduce a generic multi-port batch interface merely to support one shared release. Keep the current refusal until the complete accepted scope is represented and reviewable.

This is not just an editor change. `portedit.Result` and `workflow/preparation.Result` currently carry one target; preparation intake, branch adoption/reassociation, evidence reuse, and publication bind/policy also contain single-target assumptions. Extend those contracts together so the contribution records every changed target and restart/correction cannot drop one.

Reuse existing per-target verification plans and isolated attempts where their contracts fit. Shared-release targets are not reverse dependents: do not set `IncludeDependents` as a shortcut or run dependency discovery to manufacture their scope. Keep scope independent of verification selection, then require an explicit coverage policy for each affected target. Initially, use isolated Tart attempts for each buildable target; do not imply a shared source archive proves every subport builds. Unsupported provider coverage must produce an actionable result rather than falling back to publishing on a single passing attempt.

Acceptance requires parent/shared-subport preparation, preservation of pinned siblings, unique revision/checksum ownership, one stored source tree, durable target scope, conflicting-target isolation, cancellation/restart, corrective edits, and a publication test proving that one missing or failing required target blocks publication. This stage must not ship by deleting the current sibling-fidelity checks.

## 4. Identify dependency source archives independently of auxiliary files

Codex supplies a useful follow-up case: it has Cargo dependencies and an independently pinned V8 archive. `dependencyBase` currently requires exactly one remaining archive after removing generated dependency declarations, and regeneration later assumes one download. That restriction should eventually become a requirement for one unambiguous manifest-bearing source, not one total archive.

Keep artifact identity/ownership in `distfiles`, manifest/helper validation in `dependency`, and orchestration in `portedit`. Match the selected upstream source, extraction/worksrcdir facts, and archive contents to establish which artifact provides the manifest. Do not choose the first archive or infer ownership only from a filename. Reuse the existing complete artifact plan and preserve independently pinned auxiliary URLs, declarations, and checksums.

This follows the guard/context work and can be implemented independently of shared-subport publication if it remains a single-target contribution. Preserve baseline helper-output comparisons and maintained overrides. Missing cargo2port/go2port must still fail with the existing actionable prerequisite message.

## Package and dependency decisions

The preceding target-resolution work introduces the focused `macports/selection` boundary. No additional package or library is required for the fetch-guard increment. `macports/eval` already provides the needed boundary around native Tcl. Add focused files for fetch semantics/diagnostics and later platform observations there; do not move interpretation into `workflow` or add per-port implementations.

Keep release scope, candidate validation, context selection, and edit authorization in `macports/portedit`; `distfiles` continues to bind artifacts to exact checksum declarations. Shared observation types belong in `macports`, and only durable contribution/verification facts belong in `record`. A later package split needs a concrete independent consumer or dependency boundary, not a file-count threshold.

Continue evaluator-guided candidate generation and full forward validation. Multiple independent version inputs, new release-series creation, and coordinated Rust/bootstrap maintenance remain separate work. This pass does not introduce a generic Tcl inverse or proof engine.

## Validation and secondary findings

Use authored behavioral fixtures for positive and refusal cases, then replay pinned real Portfiles in disposable snapshots. Run the full 147-case comparison at the end of a behavior-changing milestone, and report local input discovery, candidate checks, actual archive preparation, and builds separately. Use the public-archive exercises for Terraform, Helm, gh, and Deno as regression controls, preserving their historical source versions where necessary.

The older sample also contains seventeen findings reported as missing SHA256 or ambiguous groups. Before claiming those as coverage wins, distinguish weak/absent digests from ownership ambiguity. A later checksum-upgrade feature should compute and add a strong digest while preserving correct ownership; removing the SHA256 requirement alone is not the fix.

No live PR is needed to validate the initial guard change. Exercise Wasmer through preparation and a supported-platform verification first; publication gets regression coverage and becomes a live milestone when the shared-target contracts change.
