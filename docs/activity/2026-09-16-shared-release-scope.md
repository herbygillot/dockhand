# Shared-release contribution scope

`bump --shared-release` explicitly authorizes one editable source input to move its related subports together. `assess --shared-release --version ...` and `bump --shared-release --diff` show the affected set. The initiating buildable subport remains the handle for verify/publish/status; selecting a metadata-only parent still requires choosing a release subport.

The editor records input location/value provenance, affected and protected targets, before/after version and source identity, and metadata-only parents. Related targets must share archive identity and checksum declaration owners, not merely a version string. Protected siblings retain full preparation fidelity checks. Metadata-only exclusion requires native empty-build registration with no build hooks, no configuration, and no distfiles.

Schema 17 persists the scope on immutable revisions. Candidate checkpoints retain it before branch integration. Correction, verify adoption, and branch reassociation evaluate the owning Portfile and retain membership and protected source identities. State transitions reject dropping or changing membership. The initiating target remains separate from the verification cohort; no dependent-discovery flag is synthesized.

Every buildable member receives an isolated verification attempt. One failure/cancellation blocks publication. Evidence reuse and standalone publication check the full recorded coverage, including after reopening SQLite. Source-identical single-target evidence remains reusable when no additional build target exists. Multi-target GitHub coverage remains explicitly unsupported; use Tart for this initial implementation. Per-member Xcode requirements remain part of the frozen build question.

Validation includes authored native Tcl tests for authorization, shared checksum ownership, metadata-only parents, independent pins, and corrective membership checks. Workflow integration tests cover reopening SQLite, isolated sibling requests (no sibling preinstall), cancellation, failed siblings, successful publication, and standalone-publication refusal. A later standalone passing root-only build cannot replace missing sibling coverage. Equal checksum values in separate declarations are refused before downloading. Full suite passed during implementation; final checks and real-source exercises are recorded in the [coverage report](2026-09-16-bump-coverage-validation.md).

All new implementation and tests were authored here. No v1 tests or comments were copied.
