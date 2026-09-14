# Capacity-aware Tart image inspection

Refined the planned explicit-image validation flow so capability discovery shares the normal verification admission path. Dockhand will hash a Tart source image without starting or changing it and look up capabilities by provider and immutable image digest. A cache miss will not launch a separate inspection VM.

After ordinary provider capacity admits an attempt, Dockhand will inspect the same disposable clone that will perform the verification. The probe runs before source staging and build commands. A compatible clone continues directly into the build; an incompatible clone records a prerequisite failure and releases its resource. The observation is cached for later intake and admission decisions.

Accepted work binds the selected image digest and its capability requirements. Provisioned manifests remain declared evidence and custom images remain usable without a manifest; the in-guest observation supplies the authoritative evidence. Changed image content requires another observation. The waiting driver remains responsible for reconciling admitted attempts while other work waits for capacity.

This documentation change records design only. It changes no code, schema, CLI behavior, or provider operation.

Validation: `git diff --check`.
