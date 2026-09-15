# Cache retention

Extended `gc` through existing ownership boundaries:

- Workflow enumerates old terminal jobs with bounded queries and selects terminal, unclaimed attempts for an optional local-log-cache pruning capability. Jobs, attempts, evidence, and submission identities remain unchanged.
- GitHub verification removes eligible aggregate logs and completed per-job download parts under the existing request lock. It verifies repository/run identity, honors recent cache use, skips busy downloads, and needs no network/client/authentication. Explicit subsequent log reads may download remote logs again.
- PortIndex owns collection of recognized base, complete, standalone, and candidate entries. Stage refreshes the directory's last-use time without changing index timestamps; seed reuse refreshes the seed too. Collection takes the existing profile lock, skips busy profiles, and retains lockfiles, unknown paths, and incomplete temporaries. The indexer child inherits the profile lock so an interrupted parent cannot expose a live build to collection.
- App composition adds the selected Tart index cache and shared user discovery cache to repository gc. Shared indexes are disposable across repositories; queued work can regenerate them. Active staging is protected by the profile lock and an admitted build uses its copied index.
- CLI previews identify paths/attempts and remain read-only. `filelock.TryExisting` supports nonblocking maintenance without creating paths. No new package, database schema, automatic retention loop, or generic collector was introduced.

Authored the retention implementations and regression tests; reused existing resource cleanup, provider identity, SQLite query, and file-lock boundaries. No v1 code or tests were copied.

Validation covers busy/recent caches, preview versus actual cleanup, repeat collection, lockfile preservation, symlink containment, absent paths, index timestamp preservation, terminal-job eligibility, offline GitHub collection, unchanged database evidence, and CLI composition. `go test ./...`, `go vet ./...`, and `go test -race ./internal/filelock ./internal/macports/portindex ./internal/verify/github ./internal/workflow` passed. `gmake build` succeeded. The subsequent live provisioning/verification exercise is logged separately.
