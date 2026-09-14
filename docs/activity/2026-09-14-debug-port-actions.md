# Debug output for verification build actions

The Tart guest runner now invokes build, declared tests, and installation with `-d`, retaining detailed diagnostics for actions that compile, test, or install ports. Lint and the prepared-image check for active ports keep their normal output behavior.

The verifier implementation digest includes the embedded guest program, so the change prevents earlier evidence from being reused under the new runner behavior.

`go test ./internal/verify/tart` passed. The implementation and documentation changes were authored for dockhand2; no source or tests were copied from dockhand v1.
