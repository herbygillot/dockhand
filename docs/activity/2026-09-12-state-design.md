# State-store design — 2026-09-12

## Scope

Designed the next persistence slice around the existing intake, status, single-target verification, cancellation, provider recovery, and cleanup behavior. This is documentation work; the SQLite backend and `--db` are not implemented by this change.

## Decisions

- Replace the Git ledger with backend-independent `internal/state` contracts and `internal/state/sqlite`. Keep records in `record`, policy in `workflow`, and concrete wiring in `app`.
- Read and write the affected records through scoped views and transactions. Preserve atomic claims and state changes without a separate workflow lock backend or whole-state document.
- Support multiple local repositories per database. Canonical Git common directories identify repository registrations; linked worktrees share an entry, while separate clones remain distinct. Scope selectors, status, driver work, and relationships to the selected repository.
- Replace lock-directory configuration with global `--db PATH`, defaulting to `$HOME/.dockhand/state.db`, passed through `app.Config.DBPath`. Initialize writable state lazily; status does not create missing state. Remove the old lock flags during implementation.
- Define only the tables required by current behavior, including separate submissions for recovery identity and resource ownership. Defer publication, discovery, reviews, dependent graphs, generic resource locks, and identity notes until their consumers are implemented.
- Preserve recorded Git identity and evidence if local objects disappear. Interrupted Git integration is inspected or needs attention; no source pins, Git operation journal, or atomic Git/database promise remains.

## Documentation and provenance

Authored [state.md](../state.md), including illustrative contracts, a logical schema, query and claim rules, repository isolation, SQLite lifecycle, and migration checks. Updated [architecture](../architecture.md), [components](../components.md), [CLI design](../cli-design.md), [principles](../principles.md), and [README](../../README.md) to distinguish the new design from the existing implementation. Historical activity and performance reports remain unchanged.

The design draws on the current v2 records and verification lifecycle and the storage/performance discussions. No v1 source, comments, or tests were copied. No Go code or dependencies were changed.

## Validation

Checked current-document links, whitespace, and consistency of the database and repository-scoping rules. Confirmed source files were unchanged from the start of this documentation pass. Go tests were not run for documentation-only changes. The state design specifies the transaction, multi-process, repository-isolation, recovery, and performance checks required during implementation.
