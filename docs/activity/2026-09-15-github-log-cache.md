# Resumable GitHub log caches

Completed aggregate logs are read before resolving credentials or creating an API client. Existing cache paths and trace offsets remain compatible. Missing logs are downloaded into atomic per-job files; an interrupted later job leaves earlier completed downloads available for the next read or driver process. The aggregate is assembled in job-ID order only after all jobs finish, then redundant job caches are removed. Canceled writes do not publish partial caches.

Run and job identities are checked before downloading, including duplicate job IDs. Per-request coordination serializes readers. Tests cover offline cached reads and offsets, interrupted downloads across provider restart, concurrent readers, identity mismatches, and cancellation during cache writing. No new download size or count limits are imposed.

Validation: `go test ./...`, `go test -race ./internal/verify/github`, `go vet ./...`, and `make build` passed. The provider and CLI suites also passed independently. This pass used local regression fixtures and did not trigger a live GitHub workflow or publish another PR.
