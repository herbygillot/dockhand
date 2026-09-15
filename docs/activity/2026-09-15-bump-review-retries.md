# Bump/publication review follow-up

Triaged the review against current code. Retained destination/attachment separation, early rejection of invalid GitHub configuration, and defensive single-target checks. Clarified pending PR output, missing-provider routing, and publication evidence diagnostics.

GitHub SDK primary and secondary rate limits now cross the forge boundary with their retry deadline. Confirmed publication refusals are recorded transactionally and permit another write after cooldown; ambiguous writes still require observation. SQLite rejects clearing an outstanding write without advancing a refusal counter. Old records retain their existing unresolved-write meaning. Release preparation also keeps rate-limited accepted work pending.

Workflow failures use capped exponential backoff with deterministic per-record jitter. Expected capacity, run-discovery and publication waiting use a separate interval. Job, attempt and cleanup failure counts and deadlines survive process changes; existing claims still fence external work and returned results. Cancellation can bypass verification waiting. Provider registries are authoritative, while single-provider embedded engines remain supported. An unavailable provider does not permanently terminate work that another driver could handle.

Schema 14 adds scheduling and refusal counters. Regression coverage exercises upgrade preservation, primary/secondary rate-limit responses, write retries across driver restart, unknown-write fencing, backoff persistence and reset, capacity waiting/cancellation, provider routing, and pending-publication text. Existing recovery tests now advance to recorded deadlines rather than assuming a fixed retry interval.

The scheduling state is per job/attempt/resource; this pass does not introduce an account-wide rate-limit coordinator. No dependency was added and no live GitHub workflow or PR was created.

Validation: `go test ./...`, `go test -race ./internal/workflow ./internal/forge/github ./internal/state/sqlite`, `go vet ./...`, and `make build` passed. The schema migration and GitHub credential/rate-limit contracts were also checked separately. Writable opens automatically migrate supported older schemas; explicit `db upgrade` remains available and read-only status never migrates.
