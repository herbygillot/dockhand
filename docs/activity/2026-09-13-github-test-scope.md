# Focus GitHub tests on Dockhand behavior

Reviewed the adapter tests and their upstream/CLI/workflow callers after the `go-github` migration. Removed tests for behavior now supplied by the SDK and narrowed mixed tests to the decisions still made by Dockhand.

## Removed or consolidated

- Removed SDK rate-limit parsing assertions, including primary/secondary error fields and retry timing metadata.
- Removed the oversized-response/body-closing test and its synthetic reader and tracking-body fixtures.
- Removed the oversized release-asset fixture, full-final-page request-count test, and 110-page/2,200-record test. Their handwritten limits and pagination implementation no longer exist.
- Removed the separate catalog-cancellation test; later-page failures already check that the adapter discards incomplete evidence. Publication cancellation remains covered because its uncertain-outcome classification is Dockhand behavior.
- Removed page-size, default API-version, Accept, and User-Agent assertions, plus malformed-JSON and SDK error-type checks.
- Folded the separate PR web-URL projection test into the existing publication observation test. Clone-URL validation remains covered.
- Changed the release pagination/defaults test into a small metadata-mapping test. Simplified tag catalog coverage to tag names and commits; upstream tests continue to cover discovery without release lookups.
- Reduced the annotation chain fixture to three objects, retaining nested peeling and cycle detection without tying the fixture to a retired depth limit.

Deleted `http_test.go`. The shared-client initialization test now lives in `client_test.go`, and the canceled-publication classification test lives in `publication_test.go`. The adapter no longer has a test file dedicated to a removed HTTP implementation.

## Retained

Repository binding and scope, shared-client concurrency, configured-token wiring, remote/clone validation, MacPorts livecheck links, tag-to-commit peeling, missing-ref classification, incomplete catalog rejection, and PR query/content/observation mapping remain covered. Multi-page tests remain where they exercise Dockhand's requirement to reject later ambiguity or failure before accepting evidence. Redirect tests remain because the adapter still owns the redirect guard.

Upstream selection and CLI/workflow tests were reviewed and retained: they test Portfile conventions, version selection, preparation, persisted outcomes, and publication integration rather than SDK internals.

The GitHub test files decreased from 768 to 563 lines, and from 25 to 18 top-level tests. Production code and dependencies were not changed by this cleanup.

## Validation

`go test -race ./internal/forge/github ./internal/upstream -timeout 90s`, `go vet ./internal/forge/github`, formatting checks, and `git diff --check` passed. No live API writes or VM builds were performed. Test simplifications were authored for v2; no v1 or SDK tests or comments were copied. Changes remain uncommitted alongside the SDK migration.
