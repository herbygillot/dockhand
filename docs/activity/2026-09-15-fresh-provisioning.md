# Fresh Tart provisioning exercise

Requested: rebuild current Dockhand, delete every Tart image, and exercise all supported macOS releases with both CLT and Xcode profiles.

## Starting state and scope

- Built Dockhand from `f2a18e2`, then rebuilt with the OS selector described below.
- Tart reports version 2.37.0.
- Deleted every local verification/base/golden image and every cached OCI source through `tart delete`. The inventory was confirmed empty before the first setup run.
- Existing Xcode archives are in `~/Downloads/xcode_archives`.
- Local MacPorts and the existing Dockhand state database were retained. This is a fresh Tart image exercise, not an erase/reinstall of the host.
- Running at most two setup operations concurrently. Tart serializes fresh OCI pulls through its own download lock.

## Gap found and implementation

`setup` previously bound its target OS to the host's native MacPorts platform. Supplying `--source` alone could not provision another release because validation would still expect the host's Darwin version.

Added `setup --os <release-name-or-major-version>`. Release parsing lives in `internal/macos`; application setup changes the requested Darwin version while retaining the native architecture. Tart still owns supported-image policy and default OCI/image selection. This uses ordinary CLI commands rather than a custom provisioning harness that bypasses user-visible behavior.

New code: macOS release selector parsing, setup option/flag plumbing, parser regression tests, and CLI design documentation. No new package, dependency, or state schema.

## Commands

The initial Tahoe CLT run used `./dockhand setup`. The remaining matrix uses:

```sh
./dockhand setup --os <release> --json
./dockhand setup --os <release> --check --json
./dockhand setup --os <release> --xcode ~/Downloads/xcode_archives --json
./dockhand setup --os <release> --xcode ~/Downloads/xcode_archives --check --json
```

Releases: Monterey, Ventura, Sonoma, Sequoia, Tahoe. Expected archive selection: Xcode 14.2, 15.2, 16.2, 26.3 Apple silicon, and 26.6 Apple silicon, respectively.

## Xcode regression found by the live exercise

The first Tahoe Xcode run expanded its archive but failed to locate `Xcode.app`. The preceding macOS extraction changed the working directory to an isolated temporary workspace while still passing the archive's original absolute path to `xip`. Expansion happened beside the input archive, outside that workspace.

Fixed `macos.InstallXcode` to move the staged archive into its expansion workspace and invoke `xip` there with the relative filename. Added a shell-level regression test that models expansion beside the archive, executes the actual staging/cleanup script, and stops before any host installation. It verifies both locating the expanded app and removing the temporary workspace. No host Xcode installation is performed by the test.

Stopped the Monterey Xcode attempt still using the old binary, rebuilt, and retried the failed/canceled profiles. The original Tahoe failure log is retained separately. CLT profiles and the previous successful base images were unaffected.

## Additional findings

- Monterey/Xcode 14.2 failed inside `xip` with `Block-compressed payload operation failed`, after the archive-location regression was fixed. A read-only integrity pass verified the archived member checksums for all six local Xcode archives, including the complete 14.2 payload. An isolated diagnostic retry compared the guest upload's full SHA-256 with the local file before invoking the unchanged installer; it matched exactly. That retry reproduced the same payload failure, with no other provisioning run active and about 58 GiB free shortly before failure. The root cause remains unresolved. The first failed extraction spent several minutes deleting its partial workspace; it was canceled after diagnostics were captured.
- Sequoia's first CLT profile failed during agent bootstrap with SSH exit status 125 after validating the downloaded agent checksum. The Xcode profile subsequently passed bootstrap. Sonoma's first CLT attempt showed the same exit status; ordinary CLT retries on both releases succeeded. An underlying startup/readiness issue remains a follow-up; the output-capture fix alone does not explain the exit status. The initial diagnostic contained NUL bytes.
- Code inspection found that `sshRun` shared one unsynchronized `bytes.Buffer` between stdout and stderr. Replaced that plumbing with `ssh.Session.CombinedOutput`, whose library implementation synchronizes writes. This fixes an output-capture race; it does not establish the underlying cause of the bootstrap failure.
- CLT installation and Xcode expansion can remain quiet for minutes. Guest process and disk observations were used to distinguish active work from a stall. This is a remaining progress-reporting improvement.

## Sonoma storage handling

The first Sonoma Xcode run reported only 20 GB free after the guest resize command (whose error output was discarded). A diagnostic retry showed that the expected physical store really was `disk0s2`, and its APFS container had already expanded to approximately 100 GB. The diagnostic wrapper surfaced a redundant-resize error: `The new size must be different than the existing size`.

Changed disk preparation to check free space before modifying the disk. When expansion is necessary it now retains repair/resize output, rechecks capacity, and refuses to continue if space remains insufficient. Added shell-level tests for sufficient existing capacity, successful expansion, and failed expansion with preserved diagnostics. The disk-layout assumption remains scoped to the Tart provisioning recipe; the observed failure did not justify a generic storage package or changing the disk identifier. A subsequent ordinary CLI run successfully provisioned Sonoma/Xcode 16.2.

## Results

Completed all ten profile exercises. Nine profiles provisioned successfully and passed an independent `setup --check` in a disposable clone. Each successful image reports MacPorts 2.12.6 and guest agent 0.14.1-cb39b12.

| macOS release | CLT profile | Xcode profile | Independent checks |
| --- | --- | --- | --- |
| Monterey (Darwin 21) | Passed | 14.2: failed twice during expansion | CLT passed; no Xcode image available |
| Ventura (Darwin 22) | Passed | 15.2: passed | Both passed |
| Sonoma (Darwin 23) | Passed on retry | 16.2: passed after storage handling changes | Both passed |
| Sequoia (Darwin 24) | Passed on retry | 26.3 Apple silicon: passed | Both passed |
| Tahoe (Darwin 25) | Passed | 26.6 Apple silicon: passed after archive-location fix | Both passed |

The first Tahoe CLT run exercised bare `dockhand setup`. Repeated ordinary setup and explicit checks also exercised existing-image validation. Missing-image `--check` and unknown-OS refusal were tested through the CLI.

Final Tart inventory contains 18 local images: nine ready images and nine golden copies. No VM is running and no temporary `-next` or `-check` image remains. All five freshly downloaded OCI source images remain cached. There is no published `dockhand-xcode-monterey` image; its failed candidates were cleaned up.

The isolated Monterey upload matched SHA-256 `686b9d53ca49e50d563bc0104b1e8b4f7ccfe80064a6d689965fb819bf8efe72`. The final two free-space samples before failure were 61,627,576 and 60,939,044 KiB available. Both failed Xcode runs emitted:

```text
xip: error: The operation couldn’t be completed. Block-compressed payload operation failed
```

The cleanup of those failed expansions was interrupted after capturing the error; their command records therefore show cancellation (130), not a normal `xip` exit propagated to completion. The isolated diagnostic runner only checked the uploaded archive before forwarding the original install command. It did not change the extractor or bypass validation. Sonoma's intermediate disk diagnostic wrapper deliberately surfaced the previously ignored redundant-resize error; the final successful run used the normal CLI without that wrapper.

Raw logs, input-archive integrity checks, inventories, and command/exit/duration records are under `/tmp/dockhand-provision-exercise`. This location is temporary; the conclusions and key failure evidence are retained in this report.

Validation:

- Final `go test ./...`, `go vet ./...`, and `make build` passed on the completed changes.
- `git diff --check` passed.

- Missing-image `setup --os ventura --check` refused without downloading/provisioning.
- Unknown `--os` value refused with accepted release names and versions.
- `go test ./internal/cli ./internal/macos ./internal/app` passed.
- `go test ./internal/tart/provision` passed.
- `go test -race ./internal/tart/provision ./internal/macos` passed after the SSH capture fix.


## Remaining follow-ups

- Diagnose Monterey's native Xcode 14.2 extraction failure. Local container checksums and a verified upload rule out the observed archive being truncated or altered during transfer; concurrent provisioning and exhaustion of the sampled free space do not explain the isolated retry. These checks do not establish the internal cause of `xip`'s failure.
- Investigate first-boot agent registration readiness on Sonoma and Sequoia. Exit 125 was observed twice; the retries succeeded. Do not describe this as fixed solely because diagnostics are now captured safely.
- Improve progress visibility for CLT installation and Xcode expansion, and reconsider the cost of recursively deleting a failed expansion inside a VM that will itself be deleted.
