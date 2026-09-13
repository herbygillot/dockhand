# Use GitHub SDK defaults

Simplified the preceding `go-github` migration to let the SDK own API defaults and pagination instead of preserving the handwritten client's resource policies.

## Removed

- Deleted `forge/github/catalog.go` and its generic pagination helper, item/page ceilings, page-size choices, catalog timeout, and pagination-shape checks.
- Deleted `forge/github/http.go`, including its response-size wrapper, custom success-status validation, user-agent override, API base-URL parsing, and redirect-count limit. SDK configuration now lives in `client.go` and uses `go-github` URL parsing and defaults.
- Removed the tag lookup timeout and eight-object annotation-depth cap. Exact tag resolution still validates object identity and detects cycles, with no arbitrary nesting ceiling.
- Removed the additional two-minute timeout in upstream discovery. The caller's context, including workflow call deadlines and cancellation, passes through directly.
- Removed the one-page/100-entry PR lookup restriction and redundant default sort/direction settings.

Tags and releases now consume `Repositories.ListTagsIter` and `ListReleasesIter` with nil options. PR lookup uses `PullRequests.ListIter` with only its required identity/state filters. The SDK follows pagination to completion; the adapter neither chooses page sizes nor limits how many records GitHub may return. Records are converted as they arrive, without accumulating unused release assets.

Observation validation, duplicate/ambiguous-result detection, exact-ref absence classification, and publication rejection versus uncertainty remain Dockhand concerns. Errors preserve SDK details. PR lookup checks all returned pages before accepting a unique match, and returns no partial success if a later page fails.

A small transport guard prevents write replay through redirects and credential forwarding outside the original API origin. Redirect counting and caller-supplied redirect policy remain with the HTTP client. No response-body wrapper remains. The only production constant in `forge/github` is the public GitHub web origin used to construct repository URLs.

Production code in `forge/github` decreased from 574 to 451 lines across this follow-up, including comments and blank lines. No new dependency, package, schema, CLI option, or driver path was introduced. The earlier tag-first discovery decision remains in effect.

## Validation

Focused adapter and upstream tests passed. Updated regressions demonstrate default page-size options, 2,200 releases across 110 pages, responses above the former 32 MiB cap, annotation chains deeper than the old limit, caller cancellation, and multi-page PR uniqueness/failure handling. Existing SDK error, authentication, concurrency, and publication-outcome checks remain covered. Tests of intentionally removed bounds were replaced with the corresponding supported behaviors.

`CGO_ENABLED=0 make build` rebuilt `./dockhand`; the executable's help command passed. `make test-race`, `make vet`, and `git diff --check` passed. Native MacPorts-backed tests ran; the opt-in VM acceptance test was not enabled.

No live PR writes, pushes, or VM builds were performed. The separately identified `go.setup` editing limitation remains unchanged.

All cleanup, retained transport-policy adaptation, regression coverage, and documentation were authored against v2. No v1 or SDK comments or tests were copied. Changes remain uncommitted along with the preceding SDK migration.
