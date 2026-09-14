# Git changeset boundary

Added `internal/git/changeset` as the composition layer above Dockhand's low-level Git repository mechanics. It captures committed branches and current working-tree snapshots into one source shape, binds caller-supplied bases, reports immutable path deltas, and reads the single-commit facts required by publication.

The package intentionally reports Git facts only. It does not interpret a path as a MacPorts port, infer subports, choose verification coverage, enforce publication scope, or read and write workflow state. Workflow still owns tracked-contribution association and target inference policy. MacPorts still resolves and evaluates targets. Publish still requires one verified port directory and translates the commit message into pull-request content.

Workflow verification and preparation now share changeset branch capture. Current-checkout verification uses changeset capture for source identity, provenance, modified paths, and untracked paths. Target inference obtains its explicit-base delta from the same package. Publication derives or validates a one-commit changeset there before applying its own port-directory restrictions.

Removed the higher-level `git.Repository.Contribution` method. Low-level commit-message reads, parent inspection, and changed-path comparison remain in `internal/git`; the child package composes them without making the parent depend on the child. The base is never guessed for a general delta. Only the explicitly named single-commit derivation operation obtains the candidate's sole parent, for adopting an otherwise untracked publication branch, and that observation is reused while reading the commit facts.

Updated the architecture, component map, and roadmap to record this boundary. No database schema, durable record, CLI syntax, provider behavior, or external dependency changed.

All new package code, tests, comments, and documentation were authored for Dockhand v2. Existing v2 callers and tests were refactored around the new package. No v1 code, comments, or tests were copied.

## Validation

- `go test ./internal/git/... ./internal/publish ./internal/workflow`
- `go test ./...`
- `go vet ./...`
- `make build BINARY=/private/tmp/dockhand2-changeset`
- `git diff --check`
