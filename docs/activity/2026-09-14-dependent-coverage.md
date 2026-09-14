# Frozen-source dependent coverage

Added `internal/macports/dependents` as the discovery boundary between PortIndex mechanics and workflow verification planning. `Service.Discover` materializes the requested immutable tree, stages its matching PortIndex through the existing index cache, selects direct build/library/runtime dependents, and evaluates those targets against that same source and platform. It cleans up the materialized source on return and leaves the checkout alone.

Coverage retains explicit root configurations and one default-variant question per additional dependent. Shared dependents merge their selection reasons; root variants are neither lost nor propagated into unrelated ports. Two conflicting dependents remain separate candidates for later isolated builds. Reverse discovery is deliberately direct rather than transitive; each candidate also retains its transitive indexed dependency closure.

Individual evaluation failures remain in the report, as do missing indexed dependencies and unread dependency fields. Malformed index records and cancellation abort discovery instead of returning an apparently usable partial report. Successful evaluations must match the requested source, platform, and complete target identity. The report preserves evaluated port metadata so subsequent planning can inspect environment requirements such as Xcode.

Indexed closures describe default-variant metadata, not the exact resolution in a guest. They are useful planning evidence but do not prove that an outside dependency caused a later build failure. Runtime failure attribution, revision edits, provider configuration selection, durable plan adoption, and command exposure remain separate work. This change does not automatically broaden existing bump or verify requests.

All new implementation, comments, and tests were authored for this change. Existing Git materialization, PortIndex staging/query, and MacPorts evaluation APIs are reused; no v1 comments or tests were copied. Updated the component map and roadmap to identify the remaining workflow integration.

Validation:

- Focused coverage tests passed for merged reasons, deterministic selection, duplicate roots, variant isolation, direct rather than transitive selection, conflicting candidates, closure gaps, per-target failures, cancellation, and evaluation identity checks.
- Native MacPorts evaluation passed for ordinary ports and an indexed subport sharing a different Portfile directory name.
- A Git/indexer integration fixture verified frozen source selection despite dirty checkout files, cached repeat discovery, and materialization cleanup.
- `go test -race ./internal/macports/... -count=1`, `go vet ./...`, and `make build` passed.
