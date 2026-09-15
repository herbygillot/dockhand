# Shared Actions runs across workflow jobs

The live xplr exercise exposed an integration gap: two provider requests could observe the same Actions run, but SQLite rejected the second workflow admission through `UNIQUE(provider, run_id)` on submissions. The earlier provider-only regression did not cross this boundary.

Schema 13 rebuilds submissions without that uniqueness constraint. Per-attempt live-submission uniqueness, immutable submission identity, repository foreign keys, and exclusive resource handles remain intact. The rebuild reuses the existing deferred-foreign-key validation pattern and is transactional. Migration tests preserve live submissions and linked resources, admit another observer, and reject multiple live submissions for one attempt. A new integration test admits two independent workflow jobs against the same GitHub run, cancels one, and completes the other.

Validation: SQLite and GitHub suites, the full Go test suite, and vet passed. The binary was rebuilt. The real database was backed up to `~/.dockhand/backups/state-before-github-shared-runs-20260915.db`, upgraded through `dockhand db migrate`, and passed `dockhand db check`. Existing jobs resumed without another push or another Actions run.
