# Verification preparation progress

Authored a small `progress` observation package for optional, scoped stage messages carried through the active context. Callbacks are serialized; observers do not alter operation results, create state writes, or affect provider/configuration fingerprints. The CLI installs the observer for command execution, escapes control characters, suppresses adjacent duplicate messages, and writes to stderr, including in JSON mode.

Tart reports initial image inspection and uncached hashing, then capacity reservation, cloning/startup, guest prerequisites, source materialization and packing, transfer, and verification launch. Index staging distinguishes cache lookup, mirror seeding, full generation, and incremental update. Hashing and successful generation report elapsed time. Capacity waits continue to use the existing recorded admission status. Messages describe the current process; another driver's intermediate work is not reconstructed from SQLite.

Added regressions for concurrent scoped observations, cancellation propagation, CLI JSON separation and escaping, cache hits that do not claim hashing/generation, and admission that reports transfer before calling the guest and never claims launch after a transfer failure. No v1 comments or tests were copied.

Validation passed: focused tests, `go test ./...`, `go vet ./...`, race checks for the observer/CLI sink, and `make build`.

Exercised the rebuilt CLI with `verify dockhand-fixture --branch candidate --image dockhand-base-tahoe --capacity 1 --wait --json`, against an isolated clone and fresh database. Image hashing reported 53 seconds; full fixture PortIndex generation reported one second. Every staging boundary appeared on stderr. Stdout decoded as one JSON result, with passed lint/build/test/install evidence, the resource released, and its disposable VM absent from Tart. The primary ports checkout, saved credentials, and prepared source image were not edited. Logs, result, and fixture state are retained under `/private/tmp/dockhand-progress-live-urlyoarc`; the job was `job_4TBQX6CRMDGM6DDSXXBJVEZ6QF`.
