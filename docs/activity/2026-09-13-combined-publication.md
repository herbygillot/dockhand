# Combined bump and publication

## Scope

Committed the preceding resource-retention/database-maintenance slice as `b7d4786`. Implement the next step, `bump --publish` and `bump-revision --publish`, using the existing driver and publication executor. Claude's concerns about duplicated workflow paths and incomplete command flows inform this slice; accepted input and evidence remain durable and explicit.

## Changes

- Added immutable publication destination choices to preparation intake. `--remote`, `--upstream`, and `--base` use the same resolution as standalone publication, and require `--publish` on bump commands. `--publish --no-verify` is rejected.
- One job proceeds from source preparation through verification and PR confirmation. The original accepted source stays unchanged; the prepared result revision supplies the build and publication source.
- Passing or reused verification leaves publication pending. A separate claimed pass checkpoints remote preconditions and complete content; later passes use existing expected-head pushes, operation locks, head reservations, PR checkpoints, and observation-only uncertainty recovery.
- Recheck cancellation, current revision, branch contents, and applicable evidence before new remote effects. Changing Git remotes after intake cannot redirect accepted work. Failed verification and already-current no-op bumps do not publish.
- Default attachment returns at build admission or evidence reuse. `--wait`/`--trace` follow through PR confirmation; a later `wait` or `start` resumes the same job. Status includes both the frozen destination and any eventual publication action.
- Keep SQLite per-record writes and the existing schema. Store the destination in job options; enforce agreement with a delayed publication action. Retain reused evidence through active and terminal publication states without synthesizing provider admission.

All new code and tests are authored for v2. Existing v2 destination resolution was extracted from publication planning; the push/PR executor, verification policy, and CLI flag registration are shared. Existing CLI verification fixtures were factored for reuse. No v1 comments or tests, package, dependency, or migration were added.

## Validation

The full `make test-race` suite, `make vet`, `CGO_ENABLED=0 make build`, and `git diff --check` passed. The built `dockhand` help exposes publication/destination flags on both bump commands. Targeted tests cover both bump actions, restart and lost-response recovery, destination immutability, cancellation during planning, changed/deleted branches, newer negative evidence, evidence reuse, no-update outcomes, and failed publication checkpoints. CLI integration uses real Git and SQLite, native MacPorts evaluation, and a local GitHub API fixture. It checks combined submission with `--wait` and detached submission followed by `wait`.

## Limits

The existing one-port, one-commit publication scope remains. Missing-verification scheduling for standalone `publish`, automatic rebase/squash, downstream groups, and PR monitoring are separate work. No real PR or VM is created by the integration fixtures.
