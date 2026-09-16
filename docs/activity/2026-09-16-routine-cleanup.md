# Routine cleanup in driver cycles

Implemented the second roadmap item through the existing resource lifecycle and provider cleanup capabilities. No new package or dependency was introduced. All implementation and tests in this pass were authored here; no v1 code/comments were copied.

## Policy and organization

- Verification requests now persist `KeepFailed` in job options. The CLI exposes `--keep-failed` on verification-capable actions. App/workflow binding carries it into the accepted job; it is separate from build/evidence identity. GitHub and no-verification requests cannot ask to keep a local environment.
- Terminal attempts request ordinary claimed resource release by default, including failures. Explicitly retained failures survive cycles and restarts. Successful/canceled attempts release even with the flag. Already retained resources retain their established disposition.
- Tart discards the host source-transfer archive only after staging and durable admission, with idempotent cleanup during recovery. A failed pre-admission transfer preserves the archive until its reserved execution is closed and released. Source staging and preparation scratch retain their existing operation-scoped cleanup.
- A cycle prunes at most eight released diagnostic directories whose job completion and release are both seven days old. It uses the existing `ArtifactPruner`, skips recorded build outputs, and serializes filesystem work with provider locks outside database transactions. Busy/unsupported/failed candidates back off for a day so they cannot monopolize every batch. Manual `gc` can retry immediately.
- Schema 16 adds a partial diagnostic-selection index. Released ownership remains immutable; pruning retry/error metadata may change until the permanent pruning marker is written. Compact evidence, history, source/PR identity, and operation lockfiles are preserved.
- Reusable indexes and GitHub log caches remain under explicit `gc`. Unknown orphan directories are not guessed at or swept. Cleanup runs during later driver activity, without starting a daemon solely to expire files.

## Validation

`go test ./...`, focused cleanup/lifecycle `go test -race`, and `go vet ./...` passed. Tests cover interrupted staging/launch, no restaging on retry, persisted retention across a reopened store, release without loss of failure evidence, bounded batches, build-output protection, pruning backoff, ownership immutability, provider-lock serialization, and existing claim/concurrent-collector recovery.

The schema migration was exercised on a backup containing 36 jobs, 23 attempts/evidence records, 15 contributions, five PRs, and 19 resources. Those counts survived and `db check` passed. The user's original database was not migrated or modified.

A current binary exercised real Tart with an APFS-cloned prepared Tahoe image in an isolated Tart home, a disposable ports repository, and fresh state. Measured per-run host diagnostics, excluding SQLite, shared indexes, and base images:

| Outcome | VM after settlement | Input archive | Diagnostic bytes |
| --- | --- | --- | ---: |
| Intentional build failure | released | removed | 8,340 |
| Same failure with `--keep-failed` | retained | removed | 8,340, plus the deliberately retained VM |
| Successful build with `--keep-failed` | released | removed | 22,579 |
| Cancellation after admission | released | already absent at admission | 1,973 |
| Terraform preparation refusal | none created | none retained | 0 per-run files |

These are small-fixture measurements, not a promise that every real build log is this size. Failure logs preserve the actual `port -d build` error and Tcl backtrace. Success and cancellation were exercised through the real CLI. Explicit `gc --older-than 0s` released the intentionally kept VM, pruned already-released diagnostics, and preserved the just-released failure logs for a later collection. Branches and records survived.

The original ports checkout, database, and Tart images were left intact. Exercise transcripts are kept outside the source repository; test Portfiles and Git changes are not committed to Dockhand.

## Complete ports-tree failure and automatic expiry

A second real run verified Terraform 1.16.3 from the complete ports tree with an intentionally failing `pre-build` hook, contained solely in the disposable Git clone. It used a 168,690,688-byte (160.88 MiB) source-transfer archive. After the expected failure, the archive and VM were gone; the run directory contained only 10,497 bytes of diagnostics, including the exact failure and backtrace. There were no leftover `.preparing-*` directories.

The Tart PortIndex cache retained 26,636,919 bytes separately. Discovery also uses its own shared index cache. These reusable caches are not included in the diagnostic-only figures above and remain subject to explicit `gc`; the result is not a claim that total filesystem growth was only 10 KB.

Automatic expiry was exercised with the real CLI by aging only a disposable released resource and its completed job by eight days. `start` pruned its diagnostic directory and recorded the pruning timestamp while a different repository's verification remained pending in the same database. No historical user record was altered for this test.

The full-tree run generated two cold indexes, taking 3m53s in discovery and 3m54s in Tart staging. The caches have separate roots/profile inputs; this concrete duplicated-work observation is recorded in the roadmap for a measured reuse improvement, not silently treated as equivalent cache entries here.

Exercise transcripts and measurements are preserved outside the repository at `/Users/herby/Documents/ChatGPT/Dockhand/exercises/2026-09-16-roadmap-cleanup`. The isolated Tart image/home, source checkouts, test databases, staging caches, and temporary binaries were removed after validation. The repository's `dockhand` binary was rebuilt from the committed implementation.
