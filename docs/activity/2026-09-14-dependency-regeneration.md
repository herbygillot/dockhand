# Go and Rust dependency regeneration

Implemented the second of the two requested roadmap priorities. The preceding source/checksum work was committed separately as `01862bc`.

## Changes

- Added `macports/dependency` for literal dependency declarations, archive manifest reading, optional tool discovery, helper execution, and Go/Cargo output validation. Go and Cargo interpretation have separate files; preparation orchestration stays in `prepare`.
- Added Go regeneration through `go2port get`, checked against the downloaded archive's `go.mod`. Every required module must have the expected version or pseudo-version revision and a SHA-256 checksum. Workspace, replacement, and exclusion cases require manual preparation.
- Added Cargo regeneration from the exact source archive's `Cargo.lock`. Registry checksums are compared to helper output; supported GitHub branch dependencies receive archive checksums using URLs obtained from MacPorts PortGroup evaluation. Unsupported Git source forms and registries produce explicit errors.
- Separated PortGroup-appended dependency archives from the main source in temporary frozen trees. Main version/checksum edits reuse existing preparation. Final edits restore regenerated declarations into the original Portfile and reevaluate the complete result before integration.
- Compared old declarations with regeneration of the old source to preserve maintained overrides. Missing helpers, incomplete output, tool failures, dynamic declarations, manifest-changing patches/hooks, and unsafe source configurations stop preparation. Added and removed dependencies are supported.
- Added global `--go2port` / `GO2PORT_BIN` and `--cargo2port` / `CARGO2PORT_BIN` paths, propagated through both previews and durable jobs. Setup reports the tools as optional host prerequisites without installing or requiring them for unrelated work.
- Added README guidance, a dependency-preparation document, component boundaries, and roadmap updates. Added direct dependencies on `BurntSushi/toml` and `x/mod`; existing SQLite and other dependency versions remain unchanged.

## Authorship and references

The implementation, tests, and documentation in this change were newly authored. Dockhand v1's dependency preparation was consulted for behavior and refusal cases; no v1 comments or tests were copied. The locally available MacPorts PortGroups and installed helper source informed declaration formats, archive handling, and helper limitations. In particular, helper success alone cannot prove that all dependencies were emitted, so Dockhand validates output independently against source manifests.

## Validation

- `go test ./...` passed, including native MacPorts preparation and workflow tests.
- Focused dependency/preparation tests cover complete Go and Cargo regeneration, additions/removals, Git crate URL/checksum handling, old overrides, missing versus failing helpers, partial success output, manifest parsing, unsafe archive members, source subdirectories, cancellation, and unrelated ports. Final focused tests were rerun after tightening Cargo package validation.
- `go vet ./...` and `make build` passed.
- `DOCKHAND_TEST_DEPENDENCY_HELPERS=1 go test ./internal/macports/dependency -run TestInstalledHelpers -v -count=1` passed against the installed `cargo2port` and `go2port`. Cargo used a controlled lockfile; Go used the upstream `google/go-querystring` v1.2.0 archive and generated its `go-cmp` v0.6.0 dependency.
- The live helper exercise establishes preparation behavior, not verification/build evidence. Real updates still use the usual isolated verification and publication path.

Supporting logs from this session are under `/private/tmp/dh-dependencies-*.log`. The source/checksum commit's separate activity report records its own validation.
