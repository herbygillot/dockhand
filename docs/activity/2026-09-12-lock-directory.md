# Lock directory and scoped filesystem locks — 2026-09-12

## Changes

- Replaced the global `--lockfile` flag with `--lock-dir`, retaining `-L`. The default is `$HOME/.dockhand/lock`. Relative paths resolve against the invocation directory; an explicit empty value is rejected. Help displays the directory and shell completion selects directories. The old flag is rejected.
- Renamed `app.Config.Lockfile` to `LockDir`. Application wiring initializes a shared lock directory and derives the ledger writer lock from the canonical Git common directory. Linked worktrees share `<lock-dir>/repositories/<sha256-of-common-directory>/ledger.lock`; unrelated repositories do not block each other's ledger writes.
- Introduced `internal/lock` for local resource locks independently of ledger state. Scope and operation names are validated, resource identities are hashed, and the root resolves symlinks. Initialization preserves existing files and does not acquire locks. Each acquisition uses a fresh descriptor and retains the file after release.
- Changed `ledger.Options.Lockfile` to an initialized `WriterLock`. Ledger construction no longer creates files. Existing acquisition deadlines, cancellation, atomic snapshot reads, guarded writes, and the ledger lock-timeout error remain in place.
- Updated ledger/workflow fixtures and performance-tool subprocesses to use the same scoped lock construction. Existing performance results and historical activity reports were preserved.
- Updated the README, CLI design, architecture, and component map. Fine-grained resource locking and durable workflow claims remain distinct; only the ledger writer currently uses the new facility in application wiring.

## Provenance

The advisory-lock acquisition loop was moved and adapted from dockhand2's own ledger implementation. The directory/key handling, dependency wiring, and focused tests were authored for this change. No code, comments, or tests were copied from dockhand v1. No dependencies were added.

## Validation

- `go test -race ./...` passed, including the existing ledger and workflow suites and the new lock, application, and CLI tests.
- `go vet ./...` passed.
- Focused tests exercise lock exclusion across processes, release after process termination, independent resource locks, linked worktree coordination, unrelated repository writes, cancellation, file preservation, flag parsing, and help/completion without initialization.
- A performance-harness smoke run with ten distinct sources, two writer subprocesses, one reader subprocess, and two operations per process completed all four writes and both reads successfully. This checks the updated fixture/worker lock wiring; it is not a new performance baseline.
- `git diff --check` passed. No dependencies or ledger data format changed.

## Boundaries

Processes accessing the same resource must use the same lock directory and key. Switching the directory while drivers run creates an independent lock domain. The lock utility does not implement provider admission, preparation, or publication; it supplies their future local coordination mechanism. Ledger performance optimizations discussed separately were not implemented in this change.
