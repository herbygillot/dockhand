# Tart execution — 2026-09-12

## Implemented

- Replaced the Tart placeholder with real admission, reconciliation, observation, cancellation, and release. The provider has separate lifecycle, native executor, archive/file, and guest-runner files within the existing package.
- Added `record.ProviderPool`/`ProviderExecution`, the backend-independent `state.ProviderStore` contract, and SQLite schema 2. Pools coordinate all repositories sharing a Tart home. Admission atomically counts current reservations; external running VMs also count. Closed IDs remain durable records, and terminal evidence is immutable.
- Added narrow per-submission OS locks for external Tart mutations. They live beneath the configured artifact directory, have stable inodes, and remain held by inherited command descriptors if a driver exits. Database transactions contain only state work. Capacity is released after confirmed shutdown, not after a claim or reservation expires.
- Added stopped local-image content hashing, clone/boot through host launchd, guest-agent readiness, exact committed-tree transfer, and a guest launchd runner. The submitting driver can exit while the VM/build continues; another driver can reconcile or observe the same submission. Partial provisioning is closed and cleaned; admitted guest launch intent can be completed idempotently.
- Added guest setup checks, snapshot-only MacPorts source/index resolution, platform checks, lint, declared tests, install, and structured phase/failure evidence. Required host diagnostics survive VM deletion. Interrupted shutdown resumes from the saved host result. Exited guest runners cannot remain indefinitely hidden behind a running marker.
- Wired the state backend, repository, artifact default, and longer provisioning-call deadlines in `app`. No additional package or Go dependency was needed. No CLI action handlers or persistent driver loop were added.
- Fixed a real concurrent-opening SQLite WAL setup race: establishing WAL now retries only SQLite busy/locked errors within the configured bound. Caller transaction callbacks are still never automatically replayed.

## Provenance

The provider records, interfaces, SQL migration, lifecycle implementation, native adapter, image hashing, archive writer, guest runner, and tests were authored for v2. V1 was consulted for Tart cloning, prepared-image assumptions, guest-agent transfer/execution, MacPorts build commands, and failure attribution. No v1 comments or tests were copied. The existing v2 Git materializer, workflow cycle, record types, verification judgment, and SQLite transaction engine are reused.

## Validation

Fault-injection tests cover repeated submission, changed inputs, late submission after closure, reservations shared by repositories, external capacity use, competing admissions, partial provisioning, lost launch replies, stop failures, immutable terminal results, cancellation (including a guest that has already finished), independent database writes during VM operations, resource cleanup, and mismatched guest identity. Native tests cover exact archived input, image changes despite restored modification time, and a dead runner with a running marker. A child-process test verifies that inherited lock ownership outlives the original descriptor. Migration tests cover read-only rejection of schema 1, concurrent upgrades preserving records, and complete rollback of failed migrations.

`go test ./...` and `go test -race ./... -timeout 120s` passed. `go vet ./...`, `go build ./...`, and whitespace checks passed. Targeted race checks include the additional native, migration, and cancellation tests.

The opt-in real VM test passed on Darwin 25 / arm64 using `dockhand-base-tahoe`. Driver A admitted the build and exited; driver B observed and completed the same run, and cleanup released its VM. The authored no-download port passed indexing, lint, build, declared tests, and install. The initial run took 137.63 seconds and the run with the final native observation/log-collection changes took 137.06 seconds, including hashing the prepared image in two separate processes. This is a lifecycle acceptance check, not a ports-tree performance benchmark.

```sh
DOCKHAND_TEST_TART_IMAGE=dockhand-base-tahoe \
go test -v ./internal/verify/tart -run '^TestRealTartBuildSurvivesSubmittingDriverExit$' -timeout 16m
```

The test preserves its temporary database and host artifacts for diagnostics; the test VM is released. Existing prepared images and the user's ports checkout are not modified. The final accepted run was `dockhand2-eeeb53be1bdc8a8a4a66c92a`; the prepared image digest was `sha256:bf602fc5983c40a866bcef3d23701bbf98fe40e2bf37934108fb320cdf0e3d09`.

## Limits and next work

This supports one target on a prepared local macOS image, with Tart's guest agent, passwordless sudo, MacPorts, Tcl JSON support, no installed ports, and a host GUI launchd domain. Image provisioning, remote images, artifact inputs, dependency cohorts, archive reuse, publication, action-command execution, and persistent driver residency remain unimplemented. Dependency attribution remains unknown without comparison evidence.

All cooperating drivers must use the same DB, Tart home, artifact directory, and capacity. Separate DBs and independently launched external VMs cannot share an atomic admission decision. Pool configuration is fixed on registration. Closed execution/lock records are deliberately retained; pruning needs a separate design that preserves rejection of late requests.

Image hashing reads the full VM files on the first request in a process; unchanged files use an in-memory cache after file identity, size, modification time, and change time checks. It has a material cold-start cost. Full-tree materialization and transfer are also uncached. Neither a detached build runtime limit nor recovery of guest-only results from an unexpectedly stopped VM is implemented. Host results already collected before interruption remain recoverable.

Next, connect CLI verification submission, admission/wait milestones, cancellation, and explicit resident cycles to this path. Preparation and publication can then use the same workflow owner.
