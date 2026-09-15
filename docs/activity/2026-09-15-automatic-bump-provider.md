# Prefer local Tart verification for bumps

Bump commands now select a provider automatically unless the caller chooses one or supplies provider-specific options. The application tries the existing Tart configuration/image selection first. The Tart provider distinguishes a missing executable or unsuitable image from inspection and other tool failures. Only those availability conditions permit GitHub fallback, with a setup suggestion when an image is unavailable. Xcode requirements remain part of image selection.

Provider selection stays in the application boundary, extracted from preparation into `internal/app/build.go`. Jobs retain one concrete build configuration, so Tart capacity waiting, failures, restarts and publication never add a GitHub verification submission. Explicit provider/local-policy options are preserved. Missing GitHub configuration during automatic fallback records a verification problem without a fabricated configuration, preserving no-op behavior and prepared branches. Standalone verification remains Tart by default. No dependency or schema change is needed.

Tests cover local selection without GitHub requests, fallback using mocked fork metadata, explicit providers and flags, Xcode selection/setup guidance, unexpected local errors, and the existing no-op and Tart-only publication recovery cases. README and CLI/provider documentation describe the new default and distinguish Dockhand verification from repository-triggered Actions.

Validation: `go test ./...`, `go test -race ./internal/app ./internal/verify/tart`, `go vet ./...`, and `make build` passed. Provider-choice flag tests were rerun after clarifying test-policy defaults in help. No verification job, image provisioning, branch push or PR was initiated during this pass.
