# macOS storage operations

Moved APFS capacity checks, partition-map repair, and container expansion from Tart provisioning into `macos.EnsureAPFSSpace`, using the existing explicit command target. Disk, container, path, and required capacity are positional shell arguments supplied by the caller. Tart retains its image layout choices and decides when to invoke expansion. The Xcode free-space requirement now lives with the macOS installer.

Moved the shell regression tests alongside the implementation and adapted them to exercise the exported operation with different disk identifiers and a different capacity requirement. Failure checks cover preservation of diskutil diagnostics through the returned error. No VM provisioning behavior is intentionally changed, and no new dependency or package is introduced.

Authored the parameterized operation and its argument validation; reused the existing repair/resize sequence and adapted its tests. Validation: `go test ./internal/macos ./internal/tart/provision` passed.
