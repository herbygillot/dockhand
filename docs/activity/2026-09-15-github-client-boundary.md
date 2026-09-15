# Shared GitHub client boundary

Moved authentication, device flow, credential diagnostics, SDK construction, redirect policy, rate-limit translation, and repository-name syntax into `internal/github`. Application wiring shares one client across the forge and verification adapters. Forge retains repository/catalog/publication mapping; verification now owns its Actions adapter and a private SDK-typed interface. Workflow configuration is observed inside the verifier and returned as domain build configuration, removing SDK-object inspection from app.

Existing authentication, redirect, pagination, publication, and recovery tests were retained and adjusted for their new owners. A concurrent cross-adapter test asserts that repository reads and Actions share one credential initialization. No dependencies, credential precedence, API pagination limits, provider recovery behavior, or database schema were changed.

Validation: full uncached suite, GitHub-focused race tests, vet, build, and diff checks. No remote writes or live VM operations are needed for this structural pass.
