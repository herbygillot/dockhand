# Guest-agent compatibility without version assumptions

Authored shared, bounded Tart transport probes for stdin/stdout round-tripping and command failure propagation. Setup and each newly started verification clone use the same probes after readiness, without writing guest files. Removed setup's agent release-tag comparison and verification's manifest/version comparison. The release asset remains pinned and SHA-256 verified before installation; the constant is now named AgentRelease to distinguish archive selection from executable diagnostics.

Unfamiliar, empty, or unavailable agent version output is diagnostic only. Setup captures it when available; verification retains it in per-run guest diagnostics rather than new capability identities. Cancellation still propagates. Preserved valid historical capability fingerprints so existing accepted jobs can collect results, and advanced the observation fingerprint format so old negative observations are rechecked once rather than retaining obsolete agent-version failures. Current negative observations remain cached.

Regression coverage exercises missing stdin/stdout, swallowed command failures, transport errors, cancellation, unrelated or absent version output, manifest independence, legacy fingerprint compatibility, and selective reobservation of cached failures. Existing tests retain the archive URL and checksum enforcement checks. No v1 comments or tests were copied.

Live validation: rebuilt Dockhand and ran setup --check twice against disposable clones of dockhand-base-tahoe. A temporary Tart wrapper altered only the agent --version response: first to unrelated development-build text, then to exit status 7. Both setup checks passed using the actual guest transport and toolchain; setup reported the supplied diagnostic text or an empty diagnostic respectively. The installed agent and source image were unchanged. Logs and JSON: `/private/tmp/dockhand-agent-setup-unusual.*` and `/private/tmp/dockhand-agent-setup-missing.*`.

The initial full-suite run exposed a separate pre-existing submission ordering bug; its diagnosis and fix are recorded in `2026-09-14-submission-write-order.md`.

Final validation passed: `go test ./... -count=1`, `go vet ./...`, and `make build`. The working binary was rebuilt.
