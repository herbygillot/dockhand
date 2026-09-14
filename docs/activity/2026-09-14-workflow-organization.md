# Workflow package organization

Reorganized `internal/workflow` without introducing another package or changing its exported API. Verification, preparation, and publication files now identify binding and driver execution with `_bind.go` and `_run.go` suffixes. Publication invariants shared by intake and execution live in `publication_policy.go`. Branch adoption, control execution, and execution-scope validation moved out of files that described unrelated lifecycle stages.

Moved stable lifecycle facts from workflow-private helpers onto their records. Job and attempt states report whether they are terminal, and claims report whether they are live and still own a selected generation. Retry eligibility remains private workflow scheduling policy.

Replaced JSON-derived target identity with `record.CompareTargets`, which compares the target's complete semantic selection. It treats nil and empty variant maps alike, ignores map iteration order, and distinguishes explicit enabled and disabled choices. Added a focused test for this contract.

Updated the workflow package documentation to describe the binding, intake, policy, execution, and projection seams and to reflect implemented publication behavior. Updated the roadmap to record the completed reassessment. Dependent verification planning remains outside workflow's current responsibilities.

All implementation, test, comment, and documentation changes were authored for Dockhand v2. No Dockhand v1 code, comments, or tests were copied.

## Validation

- `go test ./internal/record ./internal/workflow`
- `go test ./...`
- `go vet ./...`
- `make build BINARY=/private/tmp/dockhand2-workflow`
- `git diff --check`
