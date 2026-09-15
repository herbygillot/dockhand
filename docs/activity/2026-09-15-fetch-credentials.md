# Fetch-credential applicability

The MacPorts evaluator now checks global `fetch_credentials` as well as per-port `fetch.user` and `fetch.password`. It limits global checks to the direct master sites selected by the evaluated distfiles, including tagged auxiliary archives. Unrelated configured hosts and unused tagged sites do not block preparation.

Credential selection uses the installed MacPorts `curlwrap` implementation and its credential helper in a fresh safe Tcl interpreter. Its only curl implementation records whether authentication options were selected; it cannot download or access files, processes, or sockets. Credential values are replaced with empty/nonempty markers before entering that probe. This preserves current host matching and legacy exact-site matching without implementing a second matcher or branching on version numbers. Empty configured overrides retain MacPorts' behavior. Unsupported selector behavior or failed option evaluation becomes an inconclusive compatibility result.

Only a boolean and a fixed diagnostic can leave the evaluator. Credential fields are removed from metadata. The downloader rejects applicable or unknown credentials with instructions to prepare the update manually through MacPorts. Candidate-version validation uses the same check, so a newly authenticated source reports the actionable failure before archive download. No credentials are copied into requests, state, or logs, and no authentication support was added to the direct HTTP downloader.

New code lives in the existing MacPorts adapter and Portfile editor; no package, dependency, or database migration was introduced. The roadmap's credential-detection item is complete; its broader compatibility/capability work remains planned.

Tests cover installed host matching across schemes and ports, legacy exact site/trailing-slash matching, unrelated credentials, tagged auxiliary archives, unused sites, per-port credentials, empty overrides, redacted failures, malformed configuration, unknown selector behavior, and rejection before HTTP requests. Legacy semantics are exercised with a test selector; this does not certify an older Base installation. Tests use synthetic credentials inside disposable evaluator processes and do not modify user configuration. No v1 comments or tests were copied.

Validation passed: focused native credential regressions, the MacPorts/editor/preparation suites, `go test ./...`, `go vet ./...`, `make build`, and `git diff --check`. The binary was rebuilt before committing this implementation.
