# Portfile editing and evaluator-driven version probing

## Structure

Moved the substantive editor from `internal/prepare` into `internal/macports/portedit`. Added `internal/macports/portfile` for syntax-based candidate discovery and precise literal replacement. Git binding and final tree storage now live in `internal/workflow/preparation`; application and workflow callers use that adapter. The editor has no Git dependency. No new module dependency or database migration was required.

The adapter materializes the committed source once per resolution/preparation operation, keeping its complete resources and local files. Candidate edits are evaluated in that exclusively owned disposable workspace, with the Portfile restored after every evaluation. Intermediate observations carry no immutable source identity. Only the final edited tree is stored, independently materialized, and checked against the editor's observed result. The driver still owns branch integration and bookkeeping.

## Version behavior

Removed the calendar-specific source mapping and metadata string-replacement predictions. Upstream tag spelling and the evaluated MacPorts version are separate facts. The source interpreter supplies repository and tag-affix conventions; it does not translate dates or invert arbitrary expressions.

The syntax reader finds literal inputs in version/setup declarations, assignments, procedure returns, and command substitutions, while avoiding phase hooks and arbitrary braced data. Full source literals are tried with the real upstream value. For composed source references, shape-preserving probes can establish a literal prefix/suffix substitution. The evaluator decides the actual resulting version. A failed artificial probe is not proof that a full literal cannot be updated; a real candidate can still be tried. Failed actual evaluations remain inconclusive.

Catalog filtering uses the upstream source spelling and native Tcl livecheck regex. Candidate observations supply evaluated versions for native MacPorts `vercmp`. Every eligible candidate is considered: no assumed monotonic relationship between tag spelling and port version, and no resource cap that silently drops catalog entries. A failed candidate observation leaves discovery unknown. The selected update must have one viable edit, select the exact source tag, preserve siblings and unrelated metadata, and agree with the checkpointed version. Tag-to-commit identity is rechecked during preparation. Automatic selection records no update when the newest evaluated version does not advance the current port.

Version-derived source fields use the observed version-only baseline. Checksum regeneration is then checked against that baseline; ordinary metadata and dependencies cannot change as a side effect of rewriting hashes. Existing dependency-block validation and unchanged-block formatting preservation remain in place.

Explicit inputs use upstream spelling, with the existing optional prefix inference. A calculated version such as `20260914` does not imply an inverse mapping to `2026-09-14`.

## Validation

- Native evaluator regressions cover direct declarations, command substitutions, string-map dates, clock-validated dates, arithmetic, proc-based composed versions, inactive assignments, ambiguous edits, sibling changes, unchanged versions, failed evaluation, cancellation, and workspace restoration.
- Existing preparation integration tests cover frozen PortGroups, checkout/index preservation, local patches, Git source consistency, named/multiple archives, dependency regeneration, maintained overrides, and source-tag movement.
- Upstream tests cover evaluated ordering that differs from tag ordering, ties, no-update results, and failed observations.
- The full project tests, vet, and build passed during implementation; final validation is recorded below.
- A rebuilt CLI preview against MacPorts master `5abf07fdca48d3e5e5e4c121b864a65a89e23ec7` selected rust-analyzer tag `2026-09-14`, commit `682a84e95b5a52cf06e9822fcaa4f628738554bf`, and evaluated version `20260914`. It preserved the calculation and Cargo block, changing only the setup date and three archive checksum fields. Both explicit `bump rust-analyzer 2026-09-14 --diff` and automatic `bump rust-analyzer --diff` succeeded with the same diff. These were previews, not new verification runs or publications.

## Scope and authorship

Newly authored: the package integration boundary, reusable workspace evaluation, candidate/probe logic, evaluated-version selection callback, baseline fidelity checks, and regressions. Existing v2 download, dependency, checksum, and revision code was moved and adapted. V1 informed the probing approach; no v1 comments or tests were copied. Existing uncommitted exercise fixes were preserved. The separate MacPorts Base compatibility additions to `docs/roadmap.md` were left intact.

Remaining limits: source discovery still recognizes GitHub/GitLab conventions, and version bumps still target the primary port. The actual Terraform Portfile's non-forge, multi-subport distribution layout is not claimed as supported by this change. Proc/composition behavior is covered independently. Arbitrary Tcl inverse solving, external version inputs, and multi-input transformations remain unsupported when an unambiguous edit cannot be established. Catalog probing is sequential and may cost more for large histories; batching must preserve complete evaluated ordering and cannot assume a transformation is globally monotonic from a few probes.

## Final checks

`go test ./...`, `go vet ./...`, `make build`, and `git diff --check` passed. The final upstream cleanup was followed by its focused suite, vet, and another build. Both real rust-analyzer previews completed successfully. Existing roadmap changes match the pre-refactor backup. The dependency-formatting fix, editor/probing implementation, and existing compatibility roadmap notes are committed separately.
