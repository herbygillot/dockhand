# Retention and database maintenance

## Scope

Implemented explicit cleanup of terminal resources and standalone database backup/check commands. This follows the Claude review's concern about storage growth and the database as the sole record of workflow ownership, while retaining the existing concurrency and idempotency boundaries.

## Behavior and organization

- `gc [--older-than 168h] [--dry-run]` operates on one repository. Old retained VMs use the existing driver cleanup claim and provider release path. Older released diagnostics use an optional `verify.ArtifactPruner` capability. Jobs are never advanced by maintenance.
- `state.Maintenance` provides database-wide backup/check independently of workflow records. `state/sqlite` creates consistent SQLite snapshots, checks and syncs temporary output, and installs it without overwriting existing destinations. Backup/check require neither Git nor Tart and support older schemas read-only.
- Schema 8 records confirmed diagnostic pruning. Artifact removal leaves immutable evidence, request/job history, PR associations, and closed provider identities intact. Dry-run has no provider or write path.
- Tart pruning reuses its per-submission OS lock, verifies released ownership and generated resource paths, and removes only that run's directory through a filesystem root. Lockfiles remain stable. Missing terminal logs are reported as unavailable.
- Recovery documentation describes database loss, WAL handling, preservation of the original, standalone replacement snapshots, and required reconciliation with Git, Tart, and GitHub. There is no automatic restore over a live coordinator.

## Authorship

All implementation and tests in this slice were authored for v2. No v1 source, comments, or tests were copied. Existing v2 workflow cleanup, provider locking, and SQLite transaction utilities are reused. No dependency or package was added; two narrow capability interfaces and maintenance files live in existing packages. Current documentation was updated in place, with operational instructions in `docs/operations.md`.

## Validation

Focused maintenance scenarios pass with the race detector: live-WAL snapshots while another writer holds uncommitted work; multiple repositories; no-overwrite and competing backup destinations; canceled/damaged backups; migration and older-schema backups; read-only CLI maintenance; independent resource/log ages; live claims; normal-cycle release recovery; interrupted pruning checkpoints; competing collectors; provider identity preservation; log unavailability; and symlink containment. Pagination across more than one batch is also covered.

Final validation passed:

- `make test-race` — all packages passed; workflow completed in 168.7 seconds.
- `make vet`.
- `CGO_ENABLED=0 make build`.
- Command help for `db` and `gc`, and `git diff --check`.

Tests used temporary SQLite databases and scripted providers; no user's database, VM, artifacts, or GitHub PR was modified.
