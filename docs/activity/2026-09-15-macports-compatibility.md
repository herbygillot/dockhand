# MacPorts compatibility diagnostics and checks

Authored `macports/compatibility.go` and its Tcl companion within the existing adapter. Runtime observations include Base version, Tcl version, platform, and historical source-review coverage. Startup validates required commands and native version ordering; metadata access validates the actual worker. An inert synthetic fetch target exercises native pre/post-hook registration, record keys, wrapper names, and wrapper scope before the adapter inspects a port's real fetch implementation. Temporary target/commands are removed; hooks never execute.

Unknown fetch layouts preserve metadata access but block archive preparation with an actionable Base-specific explanation. Custom procedures and unsupported hooks also retain their refusal reason. Setup exposes host runtime diagnostics separately from the guest's installed version; evaluated snapshots retain the runtime observation. Unreviewed versions still run capability checks. No new package or version-specific shim was needed, and no v1 comments or tests were copied.

Regressions cover missing startup commands, broken version comparison, an unfamiliar version with working capabilities, missing worker options, missing target records/procedures, changed hook storage and wrapper scope, and preservation of a fetch diagnosis across the preparation boundary.

Runtime exercise: installed Base 2.12.6/Tcl 8.6.17 passed. Exported the local macports-base `v2.11.6` tag to a temporary directory and built/installed it with the CLT compiler and explicit MacOSX26 SDK, entirely under a temporary prefix. The user's source checkout and /opt/local installation were not modified. The complete evaluator suite passed against Base 2.11.6/Tcl 8.6.16 on Darwin 25 arm64. New host-based credential tests skip on that release; legacy credential tests pass. This is fixture/runtime coverage, not certification of all ports or PortGroups.

Exercise logs: `/tmp/dockhand-compat-base-2.11.6.log`, `/tmp/dockhand-compat-checks.log`; temporary build/install: `/tmp/dockhand-macports-compat-xyhv84ln`. Reproduction uses `DOCKHAND_TEST_MACPORTS_TCLSH`, documented in the compatibility guide.

Final validation passed: `go test ./...`; `go test -race ./internal/macports/... ./internal/verify/tart ./internal/verify/staging`; `go vet ./...`; and `gmake build`. The rebuilt working binary includes both this work and pre-reservation source preparation.
