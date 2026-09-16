# Bump-coverage validation

The three queued coverage items now have implementations: source-bound platform operands/context selection, manifest-source ownership independent of auxiliary files, and explicitly authorized shared-release contributions. They extend existing evaluator/editor and workflow boundaries; no new package or production dependency was needed. New implementation and tests were authored for this pass, without copying v1 comments or tests.

## Verification

- `go test ./...` passed, including native MacPorts evaluation and workflow/preparation integration tests.
- Race checks passed for `internal/workflow`, `internal/state/sqlite`, `internal/macports/portedit`, `internal/macports/eval`, and `internal/verify`.
- Final focused checks cover the native external-threshold guard, shared checksum ownership, and root-only evidence bypass. `go vet ./...`, `make build`, and command-help smoke checks passed.
- Shared-release workflow tests reopen SQLite after planning, run siblings independently without sibling preinstallation, and exercise passing, failing, and canceled cohorts through publication policy. One missing/failing sibling prevents publication, including when a later standalone root-only build passes. Scope cannot be silently dropped by a replacement revision.
- A disposable backup of the user database migrated from schema 14 to 17 and passed integrity/foreign-key checks. All old-column data matched the expected schema-15 contribution backfill; schema 17 retained all eight legacy revisions without assigning invented scopes. The copy contained 35 jobs, 23 attempts/evidence records, and nine publications. The original database remained unchanged, and the disposable database was removed after comparison.
- Corrective scope checks retain protected source identities and reject changed member sets. Metadata-only parents require native empty-build evidence; a missing archive alone does not exclude a target from verification.

The provider/forge integration exercise uses the existing controlled test implementations and real SQLite/Git. No new Tart builds or live PRs were run in this pass. The real shared-source preparations below use historical interpreter cohorts, so publishing them would not be a useful current submission. Live Tart-to-PR validation remains appropriate for the next suitable current shared-release update.

## Pinned corpus and controls

The 147-Portfile comparison uses ports commit `87ff2b89b11d1666d39a51929499c5787bebecdf` / tree `d7768980df40c495e24db97a2240149fc94b9d6a`, MacPorts Base 2.12.6, Tcl 8.6.17, Darwin 25 arm64, and the previous 25-second per-port budget. These are local assessments, not a whole-tree success estimate or build matrix.

| Outcome | Previous | This pass |
| --- | ---: | ---: |
| input-found | 83 | 83 |
| unsupported | 53 | 52 |
| unknown | 11 | 12 |

A second complete replay against the final committed implementation produced the same 147 outcomes, including after the native host-input guard. The unchanged input-found total conceals different results. Codex, libfec, mpir, and gstreamer010-gst-plugins-ugly now reach input-found. Aseprite progresses to an unresolved host-state result. abendrot, bun, warzone2100, and fldigi now report unresolved minor-version/deployment-target reads that the previous scanner missed. The py-openssl parent is unsupported as an archive target, while explicit `py313-openssl` reaches input-found; its candidate requires shared-release authorization. mrustc resolves its option threshold but still refuses unmodeled host inputs.

The seventeen targeted controls retain Wasmer, Terraform, Helm, gh, Deno, libusb-devel, iTerm2, and love0.7 behavior. Explicit py-memprof and py-ipdb candidates pass candidate checks with shared-release authorization. Candidate tags in this structural replay are supplied probes; upstream availability is not implied.

Representative combined assessment/candidate costs from the same local harness:

| Control | Native evaluator sessions | Elapsed seconds |
| --- | ---: | ---: |
| beets | 35 | 5.62 |
| py313-openssl | 28 | 4.87 |
| mpir | 29 | 4.59 |
| libfec | 28 | 4.02 |
| Wasmer | 45 | 6.88 |
| Terraform | 23 | 6.25 |
| py-ipdb | 10 | 1.79 |
| py-memprof | 7 | 1.09 |

A final py313-openssl replay after the native host-input guard used the same 28 sessions and took 4.99 seconds. These timings include local structural work, not downloads, dependency helpers, indexes, or builds. The historical beets control took 8.70 seconds, but did not record comparable session counts; no general speedup is claimed.

## Real archive preparation

Disposable snapshots were materialized from the user's ports repository; the working tree, default state database, and Tart images were not changed.

| Control | Starting ports commit | Update | Result |
| --- | --- | --- | --- |
| py310-ipdb | `77d189af775063b09aa4c8412c275ed8bc24194d` | 0.13.9 → 0.13.13 | Complete preparation; one 13,456-byte source archive. Six buildable subports plus the metadata parent recorded. |
| py36-memprof | `54194adcf37ef1c8c75835585d94ba1842073b0e` | 0.3.4 → 0.3.6 | Complete preparation; one 57,384-byte source archive. Four buildable subports plus the metadata parent recorded. |
| Codex | `95ca1d8d73b` | 0.152.1 → 0.154.0 | Manifest-source selection succeeds; dependency mapping then refuses crossterm's `rev=`-qualified Git source. No complete candidate or build claimed. |

The successful preparations downloaded actual upstream archives, regenerated source checksums, and completed final untraced fidelity checks with no unexpected changes. The stored scope includes the final checksum identities rather than the provisional pre-download values. These historical Python cohorts include obsolete interpreter versions; preparation success says nothing about their current build compatibility.

Codex establishes a concrete next task: support exact commit-qualified Cargo Git dependencies while retaining the baseline/helper comparison and maintained overrides. The existing branch-only restriction was not weakened to make the exercise pass. The pinned-V8 preparation fixture separately proves successful manifest regeneration with an untouched, undownloaded auxiliary archive.

## Boundaries

- Shared release means one editable source input with common archive/checksum owners. It does not permit arbitrary sibling edits, coordinated independent releases, or new release-series creation.
- Select a buildable subport, inspect with `assess --shared-release --version ...` or `bump --shared-release --diff`, then explicitly authorize `bump --shared-release`. The initiating name remains the verify/publish handle.
- All buildable siblings require evidence. Initial multi-target verification uses local isolated attempts; GitHub reports an explicit unsupported-coverage result.
- Darwin-major enumeration and native threshold observations remain bounded metadata coverage. Minor/deployment dimensions, mutable thresholds, and external host inputs remain unresolved unless explicitly supported.
- Manifest selection remains conservative for extraction layouts it cannot prove. There is no first-file fallback.

Raw survey/control JSON, harnesses, prepared Portfiles, and test logs are retained outside the repository at `~/Documents/ChatGPT/Dockhand/exercises/2026-09-16-bump-coverage`. The [roadmap](../roadmap.md) now prioritizes the discovered Cargo mapping gap and remaining demonstrated platform dimensions, followed by broader PR observation.
