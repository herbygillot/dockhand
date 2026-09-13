# Prepare and verify GitHub-backed Go ports

Implemented the next real-world bump slice using the ports tree's `sysutils/chezmoi/Portfile`.

## Preparation

`prepare/version_edit.go` now recognizes the version argument in `go.setup` alongside `github.setup`. Both setup forms share argument selection and existing literal-version editing, revision reset, and ambiguity checks. A separate literal `version` feeding `$version` or `${version}` into `go.setup` is supported. Package names, tag prefixes/suffixes, surrounding formatting, and comments stay in place.

`macports` now collects evaluated Go source/build metadata, including `go.version`, `go.package`, `go.domain`, `go.offline_build`, and `go.toolchain_min`. Version fidelity permits the intended `go.version` update while retaining checks on the remaining metadata, dependencies, and sibling ports.

The adapter inspects the native fetch procedure and pre/post hooks. `macports/fetch.go` recognizes the Go PortGroup's specific compatibility pre-check structurally through the existing Tcl syntax package. It accepts the standard fetch implementation with no post-fetch hooks and either no pre-hooks or recognized Go compatibility checks. Additional commands, command substitutions in messages, different conditions, or unknown hooks refuse direct archive preparation. This is deliberately limited support, not a general Tcl effect analyzer or an exemption for every Go port.

Only `fetch.archive_compatible` leaves the adapter. Raw procedure bodies are removed during metadata decoding, and no fetch hooks execute during preview or preparation. MacPorts still executes the original compatibility check during verification. The preparer continues to follow evaluated MacPorts distfiles/master sites and retains its existing checksum and tag-identity checks.

No new package, dependency, state schema, or CLI option was introduced.

## Verification fixes found by the real run

The initial VM run indexed all 41,682 ports successfully, then exposed two execution gaps:

- The guest runner read `option test.run` unconditionally. For chezmoi that variable is absent. It now uses MacPorts' `tbool test.run`, which treats an undeclared test phase as false. Metadata setup failures after indexing are also attributed to setup rather than index.
- Commands launched through this VM's guest agent inherited hundreds of open descriptors; one diagnostic shell inherited 479 while the soft limit was 256. This prevented `sudo` from creating its pipes and prevented result/log collection. All Tart guest commands now pass through one shell wrapper that closes descriptors above stderr, preserves stdin/stdout/stderr and argument boundaries, and restores the original soft limit after temporarily raising it to the existing hard limit for cleanup. The wrapper applies to staging, inspection, launch, and log collection. It does not change the base VM image or system limits.

The verifier digest includes the wrapper and advances the host protocol marker to v2. New verification records therefore distinguish this implementation from the previous runner. The corrected client collected the initial failed run's result and stopped that VM before starting verification of the same prepared branch again.

## Validation

Regression coverage exercises literal and variable-backed Go setup forms, prefix/suffix/format preservation, ambiguous or calculated inputs, native metadata and checksum preparation, recognized checks versus extra fetch/post-fetch behavior, dependency changes, and descriptor isolation with preserved standard input and literal arguments. Fixtures and checks were authored for v2; no v1 comments or tests were copied.

The real `dockhand bump chezmoi --diff` selected `v2.72.2` at upstream commit `125d00fe730fc9abf456ec312ba85b18c859bc50`. Its diff changes only the `go.setup` version and rmd160/sha256/size checksums. The archive is 2,603,694 bytes, with sha256 `977c779f616ebf3d49700ceca426d61f367e2850ff397d3ae95ca32d7f954309`.

The actual bump job is `job_W6ZAJP4QRUJO4JNZ2D4BUR67NH`. It prepared branch `dockhand/bump/chezmoi-w6zajp4qrujo4jnz2d4bur67nh` at commit `cfae63efd80f27bd307a3008f1f80dc4f146a3d3` (`chezmoi: update to 2.72.2`). That first job records the old runner's infrastructure failure; its result is preserved rather than rewritten. Verification job `job_DG225736IQ4WUIT3VZKK4WJ6D7` completed successfully against the same prepared branch with the corrected runner and the existing `dockhand-base-tahoe` image. Dependency binaries remain enabled (`FromSource: false`).

The ports checkout remains on master with its original untracked directories unchanged. No branch was pushed and no PR was created. Implementation changes are uncommitted.

`make test-race`, `make vet`, `CGO_ENABLED=0 make build`, and `git diff --check` pass.

The real verification attempt `attempt_CM7BDM2LXI6LTICYC7F3ZZPWBN` passed on Darwin 25 arm64 at 2026-09-13T20:45:32Z. All 41,682 ports indexed successfully. Chezmoi lint reported zero errors and warnings. MacPorts installed `go` and `go-1.27` from binary archives, then fetched and checked the new chezmoi source archive, built it, and installed it. Chezmoi declares no test phase, so the runner correctly skipped that phase. The provider released the successful VM after collecting evidence. The build log is retained at `/Users/herby/.dockhand/artifacts/tart/dockhand2-3933cce82ad731a4cecf7bbb/build.log`.

The implementation and regression tests in this slice were authored for v2. Existing editing, evaluation, Git integration, and verification mechanisms were extended in place; no v1 source was copied.
