# Durable revision-bump driver — 2026-09-13

## Scope and behavior

Connected `bump-revision` to durable intake, preparation, guarded branch integration, and the existing verification lifecycle. The command selects committed source from the current branch or `--branch`, captures author/platform/provider choices, and creates a new `dockhand/revbump/<port>-<job suffix>` contribution branch. `--no-verify` completes at branch creation. Default attachment waits for provider admission; `--wait` and `--trace` follow completion. `wait`, `cancel`, and `start` use the same workflow machinery.

Standalone verification no longer creates a contribution or reserves a branch for one target. Tracked-branch verification can test other ports while preserving the contribution's edited-target set. Accepted inputs stay frozen; preparation's resulting revision supplies verification input. Missing provider setup preserves the branch and produces needs-attention with the cause. Full version bumps, release discovery, checksums, publication, working-tree capture, and target inference remain unfinished.

## Organization and recovery

- `record` holds accepted preparation choices, the immutable candidate checkpoint, and job claims. No new package was introduced.
- `workflow` binds preparation, claims immutable work, writes its commit, checkpoints it, and later integrates/adopts it. The existing `prepare.Service` satisfies the small consumer-owned `SourcePreparer` interface. Deterministic parent, identity, timestamp, and commit intent prevent a same-result retry from creating a different commit.
- `git` supplies repository author lookup and branch-specific OS exclusion across the Git/SQLite gap. Short Git commands inherit the lock descriptor. Git ref transactions verify the input branch and create the destination with preconditions.
- `state/sqlite` schema 3 adds job claims/checkpoints and nullable plan/attempt revision references. It adds no tables. Populated older databases migrate transactionally without dropping execution relationships.
- `app` captures effective inputs, while Cobra shares build flags and existing attachment/progress code. No background driver, global lock flag, or alternate execution loop was added.

A prepared candidate survives interruption before integration. If the branch was created but adoption failed, a later driver can adopt the exact commit. An unexpected destination is never overwritten. Missing branches after integration may have started require attention instead of recreation. Early cancellation prevents integration; uncertain integration is reconciled while preserving a confirmed branch. Lease expiry does not release an external action, so the branch lock remains necessary alongside state claims.

Ordinary Git does not take Dockhand's branch lock. Ref preconditions protect the mutation itself, but SQLite and Git cannot promise a single atomic observation. A user can subsequently move or delete the branch; later operations must inspect it. Candidate objects are not pinned before branch creation, so missing objects require attention. Unreferenced preparation objects and persistent small lockfiles may remain after interruption/cancellation.

## Validation

The full suite passed with `go test -race ./...`; `go vet ./...` and the CLI build passed. Focused tests cover:

- Competing preparation drivers, expired claims, late results, and cancellation during preparation.
- Candidate recovery, interruption between Git and SQLite writes, moved input/output branches, missing destinations, cancellation before/after integration, and repeated completion.
- Cross-process lock contention, independent branches, release on process death, and continued exclusion while an orphaned Git subprocess finishes.
- Migration of populated schema-2 jobs, attempts, submissions, evidence, resources, and provider executions; migration rollback and foreign-key preservation.
- Native MacPorts CLI revision bumps with staged/unstaged edits preserved; `--no-verify` with an unavailable Tart executable; branch retention when verification setup is unavailable.
- Standalone and tracked downstream verification scope, exact prepared-source verification, and standalone Tart admission/reconciliation without a contribution revision.

Live checks:

- `jq` and `terraform` both bound and submitted standalone verification from `~/Source/macports-ports` at `11f22962ec196b85d04fd400eb13d7c1751b5c47`, using a temporary database. Terraform evaluated all 24 ports/subports. Neither request created a contribution or changed the ports repository.
- The compiled CLI prepared `dockhand-fixture @1.0_1` in a disposable repository and admitted a real Tart build using `dockhand-base-tahoe`. A second `wait --trace` invocation resumed that same provider run through lint, test, install, and cleanup. The attempt used the prepared result revision, while accepted input and the source checkout remained unchanged. Job: `job_UVNW2PCEO5QPI74N4VOIX7QTAZ`. JSON and logs: `/private/tmp/dockhand-revision-live-xo296b2o/`.

The first live run exposed an existing Tart observation race: the guest could publish its finished result between reading a running marker and observing the runner's exit. The adapter classified that timing as an infrastructure error. It now rereads the result after detecting an exited runner, and also checks for a newly written result in the initial no-marker path. Regression tests cover both passing and failing final results, while preserving detection of a truly abandoned running marker. The Tart race suite and affected workflow tests passed after the fix, and the repeated live run passed. The first run's host evidence remains in `/private/tmp/dockhand-revision-live-85btd8d2/`; its retained test VM was released through the normal provider/workflow cleanup path.

## Provenance

All new code, tests, and comments in this slice were authored for v2. Existing v2 Git, preparation, state, verification, Tart, and process-lifetime mechanisms were extended, including the Tart completion-race fix found during validation. No v1 code, comments, or tests were copied. `go mod tidy` promoted the already-used `golang.org/x/sys` dependency to direct; it added no dependency versions. The implementation is left uncommitted for review.
