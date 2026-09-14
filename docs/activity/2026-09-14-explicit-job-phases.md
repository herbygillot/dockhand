# Explicit job phases

Jobs now record whether preparation, verification, or publication owns their next transition. Intake assigns the initial phase from the accepted action. Branch integration advances continuing bump work to verification in the transaction that records the result revision. Passing or reused evidence advances combined bump-and-publish work to publication in the transaction that records the evidence outcome. Terminal jobs retain their last phase.

The driver cycle dispatches directly on this field. Preparation, verification planning, publication planning, publication persistence, and single-target verification reject work outside their phase, removing the previous distributed inference from action, destination, result revision, attempt state, and publication presence. Human and JSON status expose the recorded phase.

SQLite schema 9 adds the constrained phase column and backfills existing work from established checkpoints. The migration path was also changed from a hand-written conditional ladder to an ordered registry that requires contiguous schema versions. Focused migration tests cover preparation, verification, reused/passing evidence, standalone publication, and a failed schema-9 upgrade. Workflow tests cover initial phases and both phase transitions.

All implementation, migration, tests, and documentation in this change were authored for Dockhand v2. No v1 source, comments, or tests were copied.

## Validation

- `go test ./internal/state/sqlite ./internal/workflow`
- `go test ./...`
- `make build`
- `go run ./tools/stateperf -sizes 1 -iterations 1`
- `git diff --check`
