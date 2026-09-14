# Tart image validation

Added capacity-aware validation for Tart verification images. Dockhand still hashes a stopped source image without starting or changing it. It now checks a provider-wide SQLite capability cache using that immutable digest. A cache miss follows ordinary provider admission, then inspects the disposable attempt VM before any source archive is created or staged. A compatible VM continues into the same build; an incompatible VM records a blocked setup result, stops, and releases its capacity.

The in-guest observation covers the MacPorts platform, prefix and version, active ports, passwordless sudo, foreign package managers, developer tools, optional Xcode, and a declared Tart guest agent. Provisioned images use a versioned shared manifest that the runtime observation confirms. Custom images do not require a manifest. Capability observations and prerequisite problems are shared across repository registrations that use the same database and image digest.

Added environment evidence to verification results and applicability checks. Reuse now requires a matching provider, immutable image digest, capability identity, platform, usable MacPorts installation, and developer-tools profile. A cached capability identity can be frozen into newly accepted build configuration; work accepted before the first observation binds it when the provider records evidence.

Human-readable status now shows the image and capability identities plus the observed MacPorts and developer-tools profile for direct and reused attempts. JSON status carries the complete environment evidence through the existing records.

Kept provider execution payloads immutable. Tart resolves capability evidence from the cache for admitted runs, and its existing saved-result recovery path now adopts a prerequisite result if shutdown was interrupted. Added SQLite schema 11 for capability observations and migration coverage that preserves the provider execution graph.

All implementation, tests, comments, and documentation were authored for Dockhand v2. No Dockhand v1 code, comments, or tests were copied.

## Validation

- `go test ./...`
- `go vet ./...`
- `make build BINARY=/private/tmp/dockhand2-image-validation`
- `git diff --check`
