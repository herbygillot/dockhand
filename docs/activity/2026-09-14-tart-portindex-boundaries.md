# Tart and PortIndex package boundaries

## Change

Shared Tart mechanics now live in `internal/tart`. The package owns local Tart command execution, image read/write and provisioning locks, image manifests, and release-based image defaults. Image construction moved to `internal/tart/provision`; it no longer imports the verification provider. `internal/verify/tart` remains the concrete verification adapter and consumes the shared package for image identity and command mechanics.

The advisory flock loop moved to `internal/filelock`. It coordinates narrowly scoped external resources and carries no workflow or database authority. Tart defines its image lock paths on top of that primitive, while the verification provider uses it for per-submission process locks.

Frozen-source PortIndex construction, mirror reconciliation, and cache management moved from the Tart provider to `internal/macports/portindex`. The package accepts explicit Git source, platform, executable identity, mirror, cache, and destination inputs. The Tart adapter freezes those settings and asks the package to stage the resulting full and quick indexes before it creates the guest archive.

## Boundaries

Provisioning and verification now depend independently on `internal/tart`; neither owns the other's mechanics. PortIndex generation imports no verification or Tart package. The provider retains admission, guest execution, capability inspection, evidence collection, and resource cleanup. Provisioning retains the image recipe, SSH bootstrap, Xcode installation, validation, and candidate adoption.

The Tart command runner centralizes environment selection, descriptor inheritance, output capture, streaming, cancellation, and error formatting for synchronous Tart calls. Provisioning still owns its asynchronous VM process because setup must observe and stop that child directly. Verification still owns launchd integration because a submitted VM must survive the invoking driver process.

## Validation

The moved unit tests cover image defaults, image lock exclusion, mirror download, PortGroup-aware indexing, and shared-resource invalidation. Existing provider integration tests cover cache reuse, concurrent construction, incremental candidates, mirror reconciliation, and staged guest indexes through the new package boundary. The affected Tart, provisioning, PortIndex, app, and CLI suites passed after the refactor.
