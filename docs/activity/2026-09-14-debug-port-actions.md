# Debug output for all verification port actions

The Tart guest runner now includes `-d` in its common MacPorts command prefix. Lint, build, declared tests, and installation therefore retain the same detailed diagnostics. The prepared-image check for active ports also enables debug output and redirects its diagnostic stream to the build log while preserving stdout for the check.

The verifier implementation digest includes the embedded guest program, so the change prevents earlier evidence from being reused under the new runner behavior.

`go test ./internal/verify/tart` passed. The implementation and documentation changes were authored for dockhand2; no source or tests were copied from dockhand v1.
