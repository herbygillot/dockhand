# Verification performance pass — 2026-09-13

## Scope and implementation

Committed the preceding chezmoi preparation and explicit debug-build work as `8f2b4bc`. This pass addresses repeated VM-image hashing, frequent provider observations, uniform call deadlines, and misleading completion output.

- Added a disposable provider/path image-digest cache behind `state.ImageCache`, implemented by SQLite schema 7. Tart checks the stopped image and its file metadata on every lookup. Content hashing remains outside database transactions; changed, replaced, missing, or interrupted images cannot publish a valid cache entry for the old observation. Multiple repositories sharing the database share this cache.
- Moved Tart image inspection and hashing from `native.go` into `image.go`. A versioned stamp includes path, size, modification time, mode, device, inode, and disk/NVRAM change time. Configuration contents are hashed directly on each lookup, avoiding a disk rehash for config change-time updates alone. The content digest itself retains its existing meaning.
- Added operation-specific workflow deadlines and a claim grace period. Resolve, preparation, provisioning, observation, publication, and cleanup have separate budgets. Claims derive their expiry from the selected operation. These deadlines do not limit the detached build's lifetime.
- Separated normal provider observation scheduling from retry and CLI refresh cadence. Running builds schedule their next observation ten seconds later in shared state. Cancellation clears pending observation delays without taking an active claim away from another driver. Transient errors and capacity retries retain the shorter retry delay.
- Completion output now states the successful verification outcome. Reuse diagnostics appear separately during progress and remain available in detailed status. Pending-work output explains that a driver must settle results and perform cleanup.

All new cache, timing, scheduling, output, and test code was authored for v2. The hashing implementation was moved from existing v2 code; no v1 comments or tests were copied. No dependency, CLI flag, or new package was introduced.

## Validation

`make test-race`, `make vet`, a `CGO_ENABLED=0` build, and `git diff --check` passed. Behavior tests cover independent processes sharing a cache, invalidation despite restored modification time, image replacement, interruption, migration rollback, durable observation scheduling, competing drivers, cancellation, operation deadlines, and completion text. The [performance report](../performance/2026-09-13-verification-overhead.md) records measured real-image startup probes. A fresh chezmoi VM verification passed with two attached driver processes, one attempt, and confirmed VM release. The run used the preceding all-metadata cache stamp; its follow-up probe exposed a config-only change-time update. The final correction reads config contents for cache validation, with a regression test and passing real-image cache/clone probes. The build-input digest algorithm is unchanged.


Final validation passed: the full race suite, vet, pure-Go build, and whitespace check. The corrected real-image cache measured 46.468 seconds cold, 0.133 seconds in a new warm process, and 0.116 seconds after a real Tart clone. The temporary clone was deleted. The earlier full chezmoi run took 392.36 seconds, recorded one passing attempt with two attached drivers, and confirmed VM release. Detailed methodology and the distinction between build validation and final cache-path validation are in the performance report.
