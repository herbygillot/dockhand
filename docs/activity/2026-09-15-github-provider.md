# GitHub provider implementation

Added `internal/verify/github` with separate configuration, source checks, submission/recovery, observation, and logs files. It validates a committed single-port contribution and the existing MacPorts workflow matrix, records push intent in SQLite, pins a discovered run attempt, and interprets matrix completion under the workflow's own policy. It uses existing Git push and locking operations and does not dispatch workflows or reruns.

Added authenticated go-github Actions adapters, including complete pagination, attempt-specific observations, cancellation acknowledgment, and completed-job log downloads without forwarding API credentials. YAML uses an existing dependency, now declared directly. No v1 code or comments were copied.

Real temporary Git remotes and SQLite test durable recovery, duplicate submission, closed identities, preflight refusals, incomplete/negative results, and log caching. SDK adapters use an HTTP test server. The full suite and focused race tests passed. No real fork, workflow, or PR was changed.
