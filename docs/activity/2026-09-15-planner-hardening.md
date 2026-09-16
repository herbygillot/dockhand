# Planner hardening after the coverage review

## Context discovery

Reproduced successful but incomplete preparations for a multiline OS condition and a comparison assigned through `set modern [expr {...}]`: both changed the shared version but left the older-OS archive's checksum unchanged. Context discovery now scans command contents regardless of the enclosing command name or formatting. Each Darwin-major read must have an individually supported literal comparison; one recognized comparison cannot hide another unresolved read. Arithmetic/computed bounds remain an explicit uncertainty rather than a guessed boundary.

Added native preparation regressions for both missed-context cases and focused checks for unresolved aliases, mixed literal/variable bounds, arithmetic, and fractional bounds. Both affected archives are now downloaded and refreshed. The targeted profile and native preparation tests passed, including independent-pin preservation and existing architecture/OS coverage.

The scanner remains a conservative source analysis, not a general Tcl interpreter or proof of arbitrary PortGroup behavior. All code and fixtures in this pass are newly authored; no v1 comments or tests were copied.

## Host-dependent observations

The modeled evaluator now observes file opens (including process pipes), source-file loading, and directory enumeration as well as filesystem metadata queries and process execution. Relative paths are normalized in the worker during the intercepted access. Captured regular files remain permitted; external paths and opaque symlinks produce a coverage gap. Directory enumeration is conservatively reported rather than treated as a modeled platform fact. Host-access events do not record file contents.

Native evaluator regressions passed for external and captured reads/sources, relative filesystem queries, symlinks, process execution, declaration ownership, and session isolation. The native final evaluation remains uninstrumented. This expands explicit dependency detection; it does not turn a modeled context into a virtual machine or certify arbitrary environment/SDK behavior.

## Revisions across contexts

Revision reset now collects the executed literal declarations from every affected context and applies each source span once. It handles nonzero alternate revisions even when the native revision is already zero. The complete candidate still passes the existing whole-Portfile and protected-context fidelity checks before any download; a revision shared with an independent pinned release is refused.

Moved revision ownership/reset logic into `portedit/revision_reset.go`, leaving version spelling/probing helpers separate. Native regressions passed for separate OS revisions, a zero native revision with a nonzero alternate revision, protected shared revisions, independent old releases, and selected release-series subports.

## Evaluation overhead

Added an optional selected-port evaluator operation for counterfactual/discovery probes and an explicit selected-only observation mode. Baseline loading and strict/final fidelity still evaluate all siblings. Read-only discovery no longer resets revisions merely to calculate a candidate's version. Source-bound requests reuse traced baseline observations keyed by exact source contents, platform, and selection scope; candidates and final untraced checks always run again. Cancellation remains checked before cache access.

Regressions establish that a selected probe can avoid an unrelated sibling failure while full evaluation still reports it, that sibling changes still reject automatic edits, and that cache entries cannot cross contents/scope or satisfy final evaluations. The broader package run also caught and corrected lost `ErrProbeInconclusive` classification on revision-observation failures; its targeted assessment/probing regressions passed afterward.

A paired `beets` assessment against the same committed ports tree took 23.54 seconds before and 8.70 seconds after, both `input-found`. This is a local diagnostic comparison, not a timing guarantee or a full-tree extrapolation. The earlier 25-second timeout remains recorded in the original survey report.

## Survey follow-up

Replayed the same 147-Portfile sample. Investigating every changed outcome caught a missed `configure.build_arch` alias in the tightened source scanner: libewf had incorrectly stopped requesting its alternate architecture. Restored alias recognition and added a native preparation regression with separate unnamed checksum declarations under that condition. Both archives are refreshed, and libewf again reports its modeled host-filesystem dependence as unknown. Terraform's explicit `terraform-1.16` / `1.16.2` CLI assessment still passes candidate checks across native arm64 and modeled x86_64.

## Equivalent calculated-version candidates

A new arithmetic fixture (`release * 10 + 1`) exposed two relations generating the same byte-for-byte edit: numeric inversion and literal substitution. Counting those as two inputs incorrectly refused the update. Candidate evaluation now deduplicates identical edits before evaluation, while different edits still trigger ambiguity. Forward-probing and genuine-ambiguity/sibling-fidelity regressions passed.

## Cold submission deadline

A cold verification generated its complete PortIndex in 4m24s, then exhausted the original five-minute provider-call budget during VM provisioning. No build verdict was produced; reconciliation closed the partial submission and released its resource. Increased the bounded submission/reconciliation default to fifteen minutes to cover both cold source staging and VM startup. Explicit engine overrides and shorter observation/cleanup deadlines remain intact; claims automatically use the same budget plus their existing grace period.

A concurrent-driver regression advances the stored clock beyond the old deadline during a still-active submission and establishes that another driver cannot reclaim it. Existing operation-deadline/retry checks passed alongside it. The longer default also extends the maximum abandoned-claim recovery wait; splitting staging into a separately persisted phase would be a larger design change, not a requirement for this bounded correction.

## Final survey

The repeated 147-Portfile sample finishes with 83 input-found, 53 unsupported, and 11 unknown, compared with 85/54/8 before this hardening pass. Beets now completes; libfec, mpir, and gstreamer010-gst-plugins-ugly conservatively become unknown because they read Darwin-major values without individually resolvable comparison boundaries. Py-openssl changes from unsupported to unknown for the same reason. Libewf retains its original unknown result after the architecture-alias correction. These are local assessments, not successful builds or complete-preparation guarantees.

Raw results, progress, and a reusable survey harness are retained in the shared workspace at `exercises/2026-09-16-hardening`. The harness's module replacement points to the persistent project checkout; adjust that local path when reproducing elsewhere. The original survey evidence remains unchanged.

## Repeated real archive preparations

Replayed the earlier Deno 2.9.5 → 2.9.6, gh 2.100.0 → 2.101.0, terraform-1.16 1.16.0 → 1.16.2, and helm-4.2 4.2.3 → 4.2.4 preparations against the hardened implementation. All four passed with two public archives downloaded per target and final fidelity checks. The disposable corpus and historical Portfiles were the same as the [initial exercise](2026-09-15-scoped-bump-planner.md); the user's checkout and branches were not edited. These establish preparation, not builds on the modeled architectures/OS versions. Reports, digests, and final Portfiles are retained alongside the survey evidence.

## Final validation

- The complete `go test -p 1 ./...` suite passed after the final code changes, including workflow/preparation integration tests.
- `go vet ./...` passed; the project CLI was rebuilt.
- The 147-port survey and all four repeated public-archive preparations completed.
- A cold end-to-end Tart verification passed and released its VM automatically, exercising the corrected submission budget beyond its former five-minute limit.
- The project's main branch contains the individual fixes and this report. No GitHub push or PR publication was performed in this pass. The pre-existing untracked `docs/reviews/` directory remains untouched.

The out-of-scope port investigation and patch were moved outside this repository to `~/Source/jq-version-marker`, and their proposed submission was removed from the Dockhand roadmap. The unpublished documentation commit was amended to exclude those artifacts. Dockhand retains the independently applicable submission-budget fix and its validation.
