# macOS disk-image operations

Moved the APFS recovery-partition operation and its GPT helpers/tests from Tart provisioning into `internal/macos`. `RemoveRecoveryPartition` accepts the raw disk image path; its caller is responsible for offline, exclusive access. Tart still selects its `disk.img`, configures virtual hardware, and decides whether the Xcode profile requires removing recovery. The existing GPT checksum, geometry, mirrored-table checks, and write order are unchanged.

Authored the exported operation documentation and updated the macOS package description. No new dependencies or packages, disk algorithms, or provisioning policy changes.

Validation: `go test -race ./internal/macos ./internal/tart/provision`, `go vet ./...`, `make build`, and `git diff --check` passed with both extractions in place. Tests operate on temporary disk fixtures and simulated commands; no live image was modified for this refactor.
