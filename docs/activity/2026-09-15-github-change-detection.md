# Match GitHub workflow change detection

Added a Git query for added/modified paths with standard rename detection. The existing all-path scope query remains unchanged and includes deletions and both sides of moves. GitHub source eligibility now uses the workflow's AM selection for detection and the full path set for scope checks. Deletion-only, rename-only, and out-of-port changes are refused before pushing; deleting a patch alongside a modified Portfile remains supported.

Advanced the GitHub verifier digest to distinguish the tightened eligibility rules in newly built configurations. Tests cover added, modified, deleted, renamed, type-changed and mode-changed entries, literal unusual filenames, invalid/canceled queries, and source validation with real immutable Git objects. No external push or workflow run was performed.

Validation: `go test ./...`, focused cancellation/concurrency tests under `-race`, `go vet ./...`, `make build`, and `git diff --check` passed across the three fixes. The binary was rebuilt. Live fork/Actions/PR exercise remains a separate next step.
