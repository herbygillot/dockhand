# Automatic Xcode profile selection

Dockhand now reads the evaluated `use_xcode` option before choosing a verification image. Targets that require Xcode select the release-specific `dockhand-xcode-*` image by default, while an explicit `--image` remains authoritative.

The accepted build configuration and fallback evidence requirements both record the Xcode requirement. This prevents verification reuse from treating a base-image attempt as applicable to an Xcode-required target or vice versa.

The workflow binding layer gained a build resolver that runs against the already evaluated snapshot. This keeps Tart-specific image policy in the application layer and avoids evaluating the Portfile twice.

Validation covered MacPorts option decoding, default image selection, evidence requirement comparison, and the focused application, workflow, verification, and Tart test suites.
