# Extract macOS release and developer-tool operations

The tree was clean before this work. Added `internal/macos` as a platform boundary independent of Tart, verification, durable records, and state storage.

## Organization

- Moved the Darwin/macOS product-version/release-name mapping into `macos.Release` and `ReleaseForDarwin`. Tart still decides which platforms it can provision and owns conventional image names and OCI sources.
- Moved Xcode archive parsing and selection, including the existing compatibility rules and tests, out of Tart provisioning.
- Extracted developer-tool observation from verification. Provisioning and verification now use the same observation routine; each retains its own acceptance policy. Probe exit failures become reported problems; transport errors and cancellation remain errors.
- Extracted the compiler smoke test, CLT installation, and Xcode expansion/selection into explicit macOS operations. Tart retains image disk preparation, IP discovery, SSH archive transfer, and guest command transport.
- Kept MacPorts installation and all VM admission, capacity, recovery, and release behavior in their existing packages. No database schema, dependency, or CLI changes.

## Newly authored code and adjustments

Added the explicit command-target function contract, shared developer-tool observation result, provider adapters, and component documentation. The package has no implicit local command execution. Observation performs only queries; compilation and installation require separate calls.

Xcode installation takes an absolute archive path on the selected target as a shell argument and expands it in a temporary directory. Compiler checks also use a unique temporary path. Both arrange cleanup on failure. Cancellation after a failed compiler probe prevents the CLT installer from being invoked. Provisioning now also performs the same compiler-location query used by verification when validating developer tools.

Added regression tests for observation-only command execution, custom Xcode paths, malformed version output, probe failure versus transport failure, cancellation before installation, archive argument handling, and release lookup. Existing Xcode selection tests moved with the implementation.

## Validation

- Affected macOS, Tart, provisioning, and verification package tests passed.
- `go vet ./...` passed.
- `make build` passed.
- `go test ./...` passed.

No live VM provisioning or Xcode installation was performed during this refactor.
