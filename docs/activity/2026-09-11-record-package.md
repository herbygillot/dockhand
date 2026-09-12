# Record package rename and documentation — 2026-09-11

## Changes

Renamed `internal/model` to `internal/record`, updated the package declarations, and changed its imports and qualified references throughout the Go source. Updated the current component and architecture documents. Earlier activity reports retain their historical package names and file paths as provenance.

Added package documentation in `internal/record/doc.go` and fresh Go documentation comments for every exported type and constant. Documented fields where their meaning, lifecycle, optionality, or ownership needs explanation, including:

- Contribution identity across revisions and the separation between changes, jobs, attempts, resources, and publication actions.
- Complete source snapshots versus selected ports, subports, variants, and execution platforms.
- Accepted job intent, provider admission, cancellation acknowledgements, and terminal outcomes.
- Fixed build inputs, submission identity closure, claim generations, and evidence observation times.
- Failure location versus attribution to a change, including dependencies outside the edited cohort.
- Resource retention and release obligations that outlive job completion.
- Desired publication content, expected remote heads, and observed pull-request state.

The comments distinguish existing contracts from reserved or unfinished operations. No fields, constants, serialized values, or execution behavior were changed. No compatibility alias package was introduced.

## Validation and provenance

Ran `go build ./...`, `go vet ./...`, and `go doc -all ./internal/record`. Compared the Go token streams against the prior working tree after applying only the expected package/import rename, confirming that declarations and executable code otherwise remain unchanged. Checked documentation coverage for exported types and constants, formatting, whitespace, and remaining old package references in current source and design documents.

All new comments were written for the current v2 definitions and their consumers. No v1 comments or tests were copied, no tests were added, and no dependencies were changed. Existing verification-cycle work was preserved. The cycle implementation and this package rename and documentation are included in the same follow-up commit.
