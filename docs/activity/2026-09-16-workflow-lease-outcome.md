# Workflow: embedded leases and typed step outcomes

Implements the first two concepts from the [structural review](../reviews/2026-09-16-bump-machinery-structure.md) of the bump-to-PR machinery. No new package or dependency; the change is confined to `record` and `workflow`.

## Lease

`record.Lease` holds the claimable part of a job, attempt, or resource: the claim, the generation it was issued under, the consecutive-failure count, and the retry time. `Job`, `Attempt`, and `Resource` embed it in place of the four fields they each repeated, so promoted field names, JSON shape, and the SQLite columns are unchanged. `Lease.Eligible` replaces the `Claim.Live || !due(RetryAt)` gate spelled out at six sites, and `Lease.Release` clears the claim and retry time together.

`workflow` gains `take`, `fail`, and `await` on the cycle, `finishJob`, and `claimGuard`. `finishJob` is the helper formerly named `finishPreparation`, which the dependents planner was already using in the verification phase; the terminal sites in verification planning, verification settlement, publication planning, and publication running now go through it or release the lease explicitly, so a finished job no longer carries a stale claim or retry time. `claimGuard` checks terminal state, phase, and ownership in one place; the dependents planner and the publication updater previously checked no phase, so a plan or publication write could land on a job whose phase had moved. The one hand-rolled copy of `waitingDeadline` in resource cleanup is gone.

## Outcome

Recording a step now returns an `outcome` of kind `waiting`, `failed`, or `settled`, with a detail and, for failures, the error. The attempt recorder returned a string whose emptiness meant "not a failure"; the publication recorder took a variadic error list meaning "is a failure". Both are gone. The scheduler reads the kind: failures back off and keep the detail as the record's last error, waits use the wait interval and reset the failure count, settled records schedule nothing. A settled outcome can still be reported as a cycle problem, which preserves the existing report for a provider rejecting a build as unsupported and for a submission closed after partial provisioning.

Two classifications changed. An uncertain submission and an unknown run are now expected waiting whether or not the provider supplied a detail, because reconciliation exists for exactly that case; previously a provider that always filled in a detail never backed off while one that did not accumulated failures. "Publication service is unavailable" stays a backed-off failure rather than settling as needs-attention, deliberately: another driver may be configured with a publisher. Waits are still not counted, so an indefinite wait does not escalate; that needs a durable wait counter and is left for a later change.

## Checks

- New tests: lease eligibility and release, the claim guard's four refusals, `finishJob` releasing the lease, failure and waiting scheduling through the lease helpers, and the outcome constructors and reporting.
- `go test ./... -count=1` and `go vet ./...` passed after each stage. One existing test caught that the settled partial-provisioning path must keep its reported detail as the attempt's last error; the rewrite now records the reported problem for every kind, matching the previous behavior exactly.
