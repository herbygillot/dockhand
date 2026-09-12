# Verification driver cycle — 2026-09-11

## Scope

Committed the preceding workflow intake and status work as `4bddc3c` (`feat: implement workflow intake and snapshot status`). This implementation adds the next component slice: verification of one resolved target against an existing committed revision through `workflow.Engine.Cycle`. The driver owns progression, cancellation, evidence, and cleanup. The CLI and `proc` do not gain alternative execution paths.

## Changes

- Added `BuildConfig` to accepted intent and concrete `BuildSpec` inputs. Provider identity, platform, immutable environment digest, source-build choice, and explicit test policy survive process restart. Intake validates and copies supplied configuration; execution rejects missing configuration rather than consulting changed defaults.
- Added pure `verify.PlanSingle` and `verify.Judge`. Planning binds the accepted revision/source and one target; judgment requires an explicit terminal verdict and observation time and rejects contradictory passing evidence. The driver checks run identity and observation freshness.
- Implemented a bounded reconciliation pass: apply selected cancellation requests, claim one attempt action per selected job, call the provider outside the ledger transaction, record the result under the current claim, then handle eligible resource releases. Per-job errors do not stop unrelated work; ledger and caller-context errors end the pass.
- Persisted submission identities, closed identity history, retry times, attempt and resource claim generations, cancellation progress, and last errors. Leases have configurable duration and provider deadlines. Clearing a claim retains its generation; expired or replaced owners cannot commit stale outcomes.
- Distinguished capacity waiting from admission. Lost submission acknowledgements recover the existing provider run and resource handles. Unknown outcomes never authorize an immediate resubmission. Failed builds finish with retained diagnostics rather than automatically rebuilding.
- Replaced the placeholder provider `Lookup` method with `Reconcile`. Definitive absence must durably close the old submission identity at the provider before the driver can retry with a fresh identity. This fences a paused driver that reaches `Submit` after recovery or cancellation. A closed submission with partial provisioning schedules cleanup and finishes needing attention. The Tart stub now implements the revised interface; its operations remain unimplemented.
- Implemented idempotent cancellation intake through `Engine.Control`. Drivers apply requests only to selected jobs. A cancellation acknowledgement does not establish that the build stopped; a later observation supplies the outcome. Failed cancellation attempts alternate with observation. Terminal job outcomes and retained diagnostics are preserved.
- Implemented independent resource cleanup. Passing and canceled attempts request release; failed, blocked, or errored attempts retain resources unless a retention deadline is recorded. Release failures remain durable obligations. Orphan, foreign, invalid, and actively used resources are not released. Resource identity combines the provider namespace and unique resource lifetime; ownership collisions cannot overwrite existing records.
- Updated human status output for cancellation and attempt errors, and adapted platform rendering to `BuildConfig`. Updated the architecture, component status, and README with the implemented boundary and provider recovery requirements.

## Validation

Used newly authored, temporary Go checks and disposable Git repositories, outside the source repository. The scripted provider calls back into ledger reads and writes from its operations, exercising that provider work runs outside the writer lock. Checks run with Go's race detector and cover:

- Capacity waiting, confirmed admission, running evidence, explicit completion, retrying unconfirmed cleanup, and unchanged terminal job outcomes.
- Reopened engines with frozen source/configuration after the tracked change advances.
- Failure retention without automatic rebuild, invalid observations, provider/run identity mismatches, and missing or unsupported inputs.
- Uncertain submissions, recovery of admitted runs, continued uncertainty without duplication, closed identities, and partial provisioning cleanup.
- Queued, running, and uncertain cancellation; cancellation errors; scoped control application and idempotent/conflicting control requests.
- Competing cycles, expired attempt and cleanup claims, and stale results rejected even when both cycles use the same owner identity.
- A paused driver submitting after its identity has been closed, both during retry and cancellation. The provider rejects that late submission.
- Killing a separate driver process after provider admission but before ledger adoption, reopening the ledger, and recovering the original run and resource handles without resubmission.
- Cleanup ownership checks and read-only behavior for an empty cycle.

Also ran package compilation, `go vet`, formatting, and whitespace checks. No permanent tests or scripted provider were added. No real Tart VM, MacPorts build, or external publication was run.

## Provenance and remaining work

All code in this slice was authored for v2 against the existing component boundaries. No v1 implementation, comments, or tests were copied. No dependencies or packages were added.

This slice follows the intake/status commit and is committed together with the subsequent record-package rename and documentation. CLI action submission, waiting and persistent driver residency, source preparation, the real Tart provider, publication, dependent coverage, evidence reuse, and retention-management commands remain unfinished. The cycle handles one existing committed revision and one target; it does not silently reduce a cohort to one target. Transient operations use a fixed retry delay on subsequent passes; classified retry budgets and backoff remain to be designed. Real provider integration must implement the documented durable idempotency, closure, capacity, evidence-retention, and resource-lifetime guarantees.
