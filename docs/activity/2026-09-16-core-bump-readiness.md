# Core bump coverage: implementation readiness

Prepared the [next core-coverage plan](../bump-coverage.md), promoted it ahead of PR observation in the roadmap, and investigated its first concrete cases. No production behavior changed in this preparation pass.

## Sources and method

- Dockhand: `55cc2cf956d`, including the current hardened planner and OAuth registration.
- Comparison corpus: MacPorts ports commit `87ff2b89b11d1666d39a51929499c5787bebecdf`.
- Exact Wasmer failure corpus: `1a43a39ca19448b8904bb750a9a06f0d3eb30185`, matching the user's reported job.
- Runtime: MacPorts Base 2.12.6, Tcl 8.6.17, Darwin 25 arm64.

Authored a standalone investigation harness outside the application checkout. It materializes committed Git blobs, runs assessment and optional structural candidate checks in its own snapshot, checks Portfile restoration after each case, and removes the snapshot. It uses the current `eval`/`portedit` APIs. No v1 code, comments, or tests were copied.

The harness does not query upstream, download archives, run dependency generators, submit verification, create jobs, or publish. Supplied candidate versions test local editing behavior only; they are not evidence that those upstream releases exist. No new production dependency or package was added.

## New controls

Sixteen selected local assessments yielded nine input-found, three unsupported, and four unknown. Nine of those also received candidate checks: five candidate-checked, three unsupported, and one unknown. These are deliberately chosen controls, not a random coverage estimate. Per-case results are in [the control table](2026-09-16-core-bump-controls.tsv).

| Case | Observation |
| --- | --- |
| wasmer 7.4.0 -> 7.4.1 | Reproduces the reported Darwin 22 arm64 fetch-hook refusal, including on the exact reported source commit. |
| terraform-1.16 -> 1.16.2 | Passes local candidate checks and retains sibling fidelity checks. |
| helm-4.2 -> synthetic 4.2.5 | Passes local candidate checks. The original 4.2.4 request was already present in this corpus; the corrected probe is retained separately. |
| gh -> 2.101.0 | Passes local candidate checks for the existing source/binary structure. |
| deno -> synthetic 2.9.7 | Passes local candidate checks for architecture archives. |
| libusb -> synthetic 1.0.31 | Passes local candidate checks with the independent development subport preserved. |
| py311-memprof / py313-ipdb | Editable inputs are found, but candidate checks refuse the resulting shared-release changes to sibling subports. |
| py313-openssl / mpir / libfec / mrustc | Remain unknown at context discovery. The current scanner cannot distinguish all scalar/option thresholds and non-conditional OS reads. |
| Aseprite | Has real post-fetch Git work; must not be unblocked by recognizing rejection-only guards. |
| codex | Dependency regeneration rejects multiple source/auxiliary archives even before a candidate is attempted. |
| iTerm2 / love-0.7 | Local assessments remain input-found; retained as controls for older-OS pins and auxiliary sources. No new candidate or archive claim is made here. |

Terraform, Helm, gh, and Deno already passed actual archive preparation in the earlier [scoped planner](2026-09-15-scoped-bump-planner.md) and [hardening](2026-09-15-planner-hardening.md) exercises. They are regression controls rather than entirely unimplemented capabilities.

## Wasmer diagnosis

Observed the reported Wasmer source in six contexts: Darwin 22, 23, and 25 on arm64 and x86_64. Only Darwin 22 arm64 reports `known_fail yes` and an incompatible fetch hook. All six observations identify the same primary source archive URL. The Portfile's registered pre-fetch body merely reports that macOS 14 is required on Apple silicon and returns an error. Dockhand rejects it because the adapter currently accepts standard fetching plus one structurally recognized Go toolchain guard.

This supports distinguishing archive-safe rejection guards from download modifications. It does not justify discarding `known_fail` contexts, executing arbitrary hooks on the host, or marking unsupported-platform verification successful. Keep the Portfile's guard and independent pins intact.

A second relevant ordering issue is visible in `portedit/dependencies.go`: old-source download/helper validation happens before complete candidate-context planning. Wasmer's reported progress follows that order. The first implementation slice should detect local preparation barriers before spending those transfer/helper resources, while retaining the baseline comparison that protects maintained dependency overrides.

## Earlier survey triage

Reanalyzed, but did not rerun, the existing 147-case hardening survey (83 input-found, 53 unsupported, 11 unknown). Its findings include nineteen generic fetch-hook refusals, seventeen missing-SHA256/ambiguous-group refusals, eleven no-archive/metaport findings, and five unresolved Darwin-major boundaries. Findings can overlap and are not counts of future successful bumps. Many parent-port assessments need explicit subport follow-up.

The hook bucket mixes guards with real custom work; Aseprite is a counterexample to blanket acceptance. The checksum bucket mixes missing algorithms with association limitations and needs triage before weakening any policy. Replaying the full comparison becomes a completion check for the next behavior-changing milestone.

## Implementation readiness and boundaries

The first slice is general rejection-only fetch-guard recognition, useful context diagnostics, and pre-download candidate planning. Existing `macports/eval`, `macports/portedit`, `macports/distfiles`, and `macports/dependency` boundaries are adequate. The next slice improves context evidence; shared-subport editing then requires contribution scope and verification/publication coverage together. Current publication checks deliberately require one contribution target, so removing only the editor's sibling refusal would be incomplete.

Detailed sequence, preservation rules, refusal cases, and acceptance criteria are in the plan. PR observation and OAuth refresh tokens remain follow-ups behind the user-selected core workflow work.

## Evidence and validation

Raw observations, tool source, module files, runtime/source identity, progress, and reproduction instructions live at `~/Documents/ChatGPT/Dockhand/surveys/2026-09-16-core-bump-readiness`. The earlier survey remains at `~/Documents/ChatGPT/Dockhand/exercises/2026-09-16-hardening`.

The control harness, corrected Helm probe, and exact-source Wasmer replay completed successfully as investigations, including expected unsupported/unknown outcomes. Portfile restoration checks passed. No production code was changed, so the full application suite was not repeated for these documentation changes. Markdown links and `git diff --check` were checked. The existing untracked review directory/logo and ports checkout files were left untouched.

## Target-selection correction

The user clarified that a named subport must be accepted as the single positional target, without a public `--subport` flag. Inspected explicit resolution and grouped survey selection: the former searches directory names, while the latter already carries indexed subport names into their owning Portfiles. Added common source-bound name resolution and eventual flag removal as the prerequisite in the implementation plan. This changes the planned CLI contract; the current executable still has the flags until resolution is implemented. Internal subport identity remains necessary for MacPorts evaluation and stored verification evidence.
