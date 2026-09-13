# Explicit debug builds during verification

The Tart guest runner now invokes `port -d build` after lint and before declared tests or installation. The command retains the existing noninteractive option, accepted port directory, subport, variants, and optional source-build policy. Dependency binaries remain enabled by default.

The existing step executor captures stdout and stderr in `build.log`, which is streamed by `--trace` and retained with verification evidence. A build failure stops subsequent tests and installation and records a failed build step. The embedded guest script participates in the verifier digest, so older verification evidence cannot be reused across this execution change.

This is an in-place change in `internal/verify/tart/guest.tcl`, authored for v2. No dependencies, interfaces, state schema, or CLI flags were added. The component documentation describes the updated phase order.

Validation: `go test -race ./internal/verify/tart -timeout 90s`, `make build`, and `git diff --check` pass. The local `dockhand` binary has been rebuilt. The opt-in VM acceptance test was not rerun for this change. Changes remain uncommitted.
