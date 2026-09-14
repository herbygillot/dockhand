# Publication authentication and preflight

## Scope

Implemented the first publication follow-up after an authenticated Git push was followed by an anonymous GitHub PR request. Dockhand can now reuse an active GitHub CLI login as well as environment or explicitly supplied credentials, and it verifies the API identity before accepting or executing publication work.

## Implementation

- Added a small credential-source boundary to `forge/github`. Production discovery checks an explicit embedded credential first, then `GH_TOKEN`, `GITHUB_TOKEN`, and `gh auth token --hostname github.com`. A custom API origin does not receive default `github.com` credentials.
- Added a `go-github` authenticated-user check. The resolved token and authenticated client stay in process memory; the identity request is repeated for each preflight. Errors do not include token contents.
- Required authenticated GitHub clients for PR creation and updates while leaving repository, tag, release, and PR observations usable as public reads.
- Added `publish.Service.Preflight` and invoked it before standalone publication acceptance and combined bump/publication destination binding.
- Repeated preflight in the driver immediately before pushing a branch and immediately before creating or updating a PR. A missing or rejected credential before either effect settles the job as needing attention without marking that effect as started; transient GitHub failures remain retryable. Observation-only reconciliation of an uncertain prior PR write remains independent of a fresh credential.
- Split standalone publication planning from binding so `publish --dry-run` still creates no job and can inspect public repositories without credentials.
- Updated CLI help and design documentation to describe the implemented credential order and the remaining native `dockhand auth login` work.

All code and tests were authored for v2. No comments or tests were copied from dockhand v1. No credential was written to SQLite, an activity log, or a test diagnostic.

## Validation

Focused tests cover environment precedence, GitHub CLI fallback, custom-origin isolation, identity validation, token redaction, transient-failure classification, preflight before acceptance, preflight before both remote effects, and credential-independent recovery of an uncertain PR result. CLI integration tests cover unauthenticated dry runs, rejection before job acceptance, and authenticated combined and standalone publication.

`go test ./...`, `make test-race`, `make vet`, `CGO_ENABLED=0 make build`, and `git diff --check` passed.
