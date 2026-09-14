# State and workflow policy boundary

The state boundary now distinguishes persistence integrity from workflow policy. `state` and `state/sqlite` continue to enforce repository scope, required relationships, immutable identities and accepted job intent, monotonic phases and external-effect checkpoints, claim shape, and lifecycle consistency. Workflow owns evidence applicability, reuse eligibility, publication authorization, remote preconditions, and retention-age decisions.

SQLite no longer decides whether a referenced verification result is passing, matches a job's source tree, has an allowed build shape, or excludes local attempts. It verifies that the referenced attempt exists in the same repository and keeps the reference immutable. Selection and complete input comparison already occur in the workflow transaction that records reuse.

Publication action policy moved from `PutPublication` into one workflow validator. Standalone and combined publication call it before recording intent. The publication handler calls it again before claiming previously recorded work; a mismatched action is settled as needing attention before any Git or forge effect. SQLite retains the action's repository-qualified job/change relationship, publication phase, immutable intent, and monotonic push/write checkpoints. A fault-injection test alters an action between workflow validation and persistence and proves that a later cycle refuses it without pushing or writing a pull request.

Resource pruning remains guarded by the store's one-way released-resource rules. These checks prevent an impossible durable record: pruning cannot precede confirmed release, released ownership cannot change, and a pruning timestamp cannot be erased. Workflow still decides whether the job and attempt are terminal, whether the retention age elapsed, and whether preserved evidence names artifacts.

The state and component documents now describe this boundary directly. Nearby stale implementation statements were corrected: the data model is at schema 9, combined publication and image-free verification reuse are implemented, and the README no longer describes the project as initial groundwork.

All code, tests, comments, and documentation in this change were authored for Dockhand v2. No v1 source, comments, or tests were copied. No schema or dependency change was required.

## Validation

- `go test ./internal/state/sqlite ./internal/workflow`
- `go test ./...`
- `make build`
- `git diff --check`
