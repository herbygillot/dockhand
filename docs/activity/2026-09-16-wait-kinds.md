# Wait kinds and budgets

The counted-waits change made unresolved waits visible and backed them off uniformly. Waits are not uniform, so this gives each kind its own schedule and, where indefinite waiting would hide a fault, a budget.

## Kinds

| Kind | What is waited for | Interval | Budget |
| --- | --- | --- | --- |
| capacity | a provider slot for a queued attempt | wait interval, flat | none |
| build | a running build's outcome | observe interval, flat | none |
| cancellation | the provider acknowledging a cancel | retry delay, flat | none |
| reconciliation | the provider confirming whether a submission exists | doubles to five minutes | 20 waits |
| forge | the forge reflecting a pushed branch or a pull-request write | doubles to five minutes | 20 waits |
| release | the provider confirming a resource release | doubles to five minutes | 20 waits |

The unbounded kinds are bounded by something external and expected: a slot frees, a build ends, a cancel is acknowledged. The budgeted kinds wait on an answer the other side should give within a few looks; twenty waits with doubling is roughly eighty minutes, after which the silence is a fault rather than a delay.

## Change

`waitingFor` takes a `waitKind`, carried on the outcome. `waitingDeadline` takes the kind and returns whether a budgeted kind has spent its budget; `await` on the lease returns the same. Every recorder names its wait: capacity on a queued attempt, build on admission and observation, cancellation after a cancel is sent, reconciliation for uncertain submissions and unknown runs, forge for the three publication waits, release for unconfirmed cleanup. A cancel request overrides an attempt's kind so cancellation is observed promptly, replacing the runner's previous special cases for running and canceling attempts.

Exhaustion settles visibly. A verification attempt finishes as errored with "no progress after N reconciliation waits: <detail>", so the job needs attention through the ordinary settlement. A publication job finishes as needing attention with the same shape; an uncertain pull-request write stays uncertain rather than being marked as needing attention, since it may still have landed. A release obligation is kept with the detail and polled daily instead of every few minutes, and gc and status show it. Wait counts reset on failure and on settlement.

## Checks

- Unit: each unbounded kind keeps its flat interval across fifty waits and never exhausts; each budgeted kind grows monotonically, respects the ceiling, and exhausts on the twenty-first wait; the exhaustion detail names the kind and count.
- Integration: a provider that answers every reconciliation with an unknown run drives one submission through twenty-one waits, then the attempt is errored, the job needs attention, the detail names the reconciliation waits, and the submission was never repeated.
- `go test ./... -count=1` and `go vet ./...`: passed.

## The count belongs to one kind

The first version carried the wait count across kinds: three reconciliation waits followed by admission counted toward the build's waits, and a budgeted kind could inherit waits from an earlier one. `record.Lease` now records `WaitKind`, schema 19 stores it as `wait_kind` on jobs, attempts, and resources, and `await` zeroes the count when the kind changes. Failures and settlement clear both. Status names the kind: "waiting: 3 consecutive forge waits; next look ...". A test drives fifteen reconciliation waits, one build wait, and twenty-one forge waits on one lease and checks that only the twenty-first forge wait exhausts the forge budget.

The real development database and the scratch databases were migrated from 18 to 19 after a backup; records and status were intact.
