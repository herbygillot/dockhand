# Read-only outdated capability

Accepted review 7's finding that `app` had acquired the full `outdated` workflow.
Moved committed-HEAD capture, temporary workspace lifetime, indexed and explicit
selection, per-port probing, and observation aggregation into `internal/outdated`.
Its `Service`, `Selection`, `Result`, and `Port` types describe that capability.

`app.Outdated` now constructs the repository, native evaluator, concrete upstream
adapters, and index/cache configuration, then invokes the capability. CLI consumes
its result and selection types and retains parsing, rendering, and unknown-result
exit behavior. No compatibility aliases or generic discovery framework were
added. The new package has a package overview and a component dependency rule.

Existing CLI regressions pass for current/update/unknown results, dirty-checkout
exclusion, absent state, zero source downloads, maintainer/category selection,
subport/coverage gaps, empty matches, and duplicate explicit selectors. Direct
capability tests cover source isolation, unknown-result aggregation, temporary
workspace cleanup, and validation/cancellation before dependency use.

No command, JSON result shape, persisted state, source-selection policy, or
publication authority changed. No v1 code or tests were copied.
