# Contributor-facing README

Rewrote the README for MacPorts contributors submitting updates through their own forks. It now opens with a prominent prerelease warning, uses the tagline “From upstream release to submitted port update,” and progresses through building, workspace/image setup, previews, verification, publication, manual edits, and resuming work.

Moved detailed configuration, authentication, provisioning, verification, and publication behavior into `docs/usage.md`. Moved developer build/test and service-boundary notes into `docs/development.md`. Existing activity and design documents retain their detailed history; the README no longer lists them individually. Corrected the moved combined-publication explanation to account for automatic selection of setup's default image.

Copied the original light/dark PNG logo assets and the README's theme-aware picture element from the previous implementation without modifying the images. Examples were checked against the current CLI help, Makefile, module requirements, and authentication/preparation implementations. No runtime behavior changed. Changes are intentionally uncommitted for review.
