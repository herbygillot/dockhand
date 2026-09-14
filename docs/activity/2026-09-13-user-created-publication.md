# Publication of user-created branches — 2026-09-13

## Scope

Connect verified manual branches to the existing publication workflow, addressing the human-edit gap in Claude's second review. Keep one commit in one port directory, explicit publication, immutable accepted source/evidence, and existing publication recovery. Do not add a track command, package, dependency, or schema migration.

## Implementation

- Git exposes single-parent inspection, reused by contribution validation.
- Publication derives a manual contribution's parent and changed Portfile directory. Existing remote-base validation confirms ancestry and rejects multiple unpublished commits or already-merged work.
- Verification lookup can discover the newest terminal target/configuration for an exact tree and Portfile within the current repository. Failure remains failure; subport and variant choices remain the recorded choices shown by the plan.
- Acceptance creates change, revision, request, job, and publication intent in one transaction. Verification alone and publication dry runs do not adopt branches.
- Binding rechecks the local branch after preflight. Later branch changes are handled by the existing driver's checks before remote effects.

All new code and tests are authored for v2. Existing v2 publication content extraction moved from `plan.go` into `source.go`; single-parent inspection moved into Git object handling. No v1 comments or tests were copied.

## Validation

Focused publication, Git, state, CLI, adoption, and recovery tests passed under the race detector. Direct `publish` tests cover auxiliary-file edits, empty or multi-port changes, root commits, and merges. Workflow tests cover atomic adoption/rollback, independent SQLite connections racing acceptance, applicable evidence, subports/variants, branch changes, and recovery after a lost PR response. The CLI test uses real Git repositories and SQLite with a local GitHub API fixture; a dry run leaves the branch untracked and a later publication completes through the normal driver. `make test-race`, `make vet`, `CGO_ENABLED=0 make build`, and `git diff --check` all passed. Working-tree evidence followed by a committed message rewrite is also covered. GitHub interaction was tested with a local API fixture; no real PR was created or updated.
