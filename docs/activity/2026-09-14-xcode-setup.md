# Full Xcode setup profile

## Scope

`dockhand setup --xcode <path>` adds an explicit full-Xcode provisioning profile. The path may identify one release `.xip` archive or a directory. Directory selection chooses the newest release compatible with the native macOS version, prefers Apple-silicon archives when the same version is also available as universal, and ignores pre-release filenames.

The profile has separate conventional base and golden names, such as `dockhand-xcode-tahoe` and `dockhand-golden-xcode-tahoe`. Supplying Xcode while naming a conventional `dockhand-base-*` image is rejected. Ordinary verification still defaults to the smaller Command Line Tools image; the Xcode image is selected explicitly with `--image` until verification profile selection is implemented.

## Provisioning mechanics

The Xcode archive is selected before the setup lock is acquired. The new candidate disk is enlarged to 100 GB. On images whose recovery partition blocks APFS growth, setup validates both GPT headers, table checksums, table agreement, geometry, and the unique Apple APFS recovery type before changing any bytes. It clears that entry in both tables and recomputes both entry-table and declared-header checksums. The guest repairs the partition map and expands its APFS container before receiving the archive over the existing SSH provisioning channel.

The guest expands the archive, installs `/Applications/Xcode.app`, selects its developer directory, accepts the license, and completes first-launch installation. The setup manifest records the exact selected Xcode version. Validation requires the selected directory and exact `xcodebuild` version for an Xcode profile, while the base profile requires `/Library/Developer/CommandLineTools`.

The compatibility ceilings and disk-growth mechanism were informed by the first Dockhand implementation. The v2 code, comments, tests, profile separation, selection preference, validation contract, and documentation were authored for this architecture; no v1 comments or tests were copied.

## Validation performed

The available archives were located in `~/Downloads/xcode_archives`. Tahoe selected `Xcode_26.6_Apple_silicon.xip`. A real isolated provisioning run created `dockhand-xcode-tahoe-smoke` and its golden image from the cached Tahoe vanilla OCI source. It enlarged the guest filesystem, installed Command Line Tools, copied and expanded the 2.2 GiB archive, completed Xcode first launch, installed MacPorts 2.12.6, and validated Darwin 25 arm64, guest agent 0.14.1-cb39b12, and Xcode 26.6. A later `setup --check` repeated validation through a disposable clone.

An isolated end-to-end verification then built `sysutils/macos-trash`, whose Portfile sets `use_xcode yes`. The fresh source-build job `job_42VLG2RMW2BUJSBBDDSD63BTWL` completed with passing attempt `attempt_FFLKLKKQRSZBCFBC47ILZDCAA6`. Its build log showed MacPorts selecting Xcode Clang, setting `DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer`, selecting the Xcode 26 SDK, executing `swift build`, and completing the Swift compilation.

Focused tests cover per-release archive selection, architecture preference, explicit-archive compatibility, prerelease rejection, profile naming and separation, provisioning order, and GPT repair including checksum and malformed-copy refusal. The full Go test suite, `go vet ./...`, and `make build` pass.

The end-to-end run also measured a substantial unrelated cost: each clean verification VM builds and, with trace enabled, prints an index for the complete ports tree before reaching one target. Reducing or caching that per-attempt PortIndex work remains a separate verification-provider improvement.

The acceptance output also exposed a human-status formatting defect: the sanitizer converts values to strings before interpolation, while the working-tree file count still used an integer format verb. The renderer now uses the string verb and has a regression test.

The two isolated Xcode images, disposable verification images, temporary state database, artifacts, and released-resource lockfiles were removed after the checks. Existing base, golden, OCI, and unrelated worker images were left unchanged.
