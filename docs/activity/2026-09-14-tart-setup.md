# Tart setup and image provisioning

## Scope

`dockhand setup` now prepares the conventional Tart image for the host's native MacPorts platform. The command is state-independent: it does not open SQLite, register a repository, submit a workflow job, or require a ports checkout. It does use the selected local MacPorts installation to obtain the Darwin major version and build architecture.

The first profile supports arm64 Darwin 21 through 25 and MacPorts under `/opt/local`. It maps those releases to names such as `dockhand-base-tahoe` and sources such as `ghcr.io/cirruslabs/macos-tahoe-vanilla:latest`. The recipe installs Tart guest agent 0.14.1 from its universal Darwin release archive after checking the pinned SHA-256 digest, installs Apple's Command Line Tools when a compile probe fails, and installs MacPorts 2.12.6 by default from the official package distribution. Full Xcode and alternate-prefix profiles are intentionally deferred.

`setup --check` validates an existing image without pulling or installing. Ordinary setup validates an existing base in a disposable clone or provisions a missing one. `--rebuild` always creates a replacement candidate first. `--image`, `--source`, and `--macports-version` select explicit recipe inputs. Human output and JSON describe the checked result, while step progress stays on stderr.

## Boundaries and recovery

The new `verify/tart/provision` package owns the image recipe, temporary resources, validation, and native Tart/SSH mechanics. `app.Setup` supplies platform discovery and configuration. `cli` owns flags and rendering. Existing provider code remains responsible for attempts and execution resources. No provisioning record or schema was added because this synchronous host operation has no accepted workflow obligation to resume.

Provisioning constructs `-next` base and golden images and validates the candidate before adoption. Failure before adoption leaves an existing base and golden image intact. A failed final base clone retains the proven candidate. A completed golden can restore a missing base on a later ordinary setup. Cleanup stops and removes owned temporary VMs when their status is known.

Setup commands take a per-image provisioning lock for the recipe. Verification takes a shared image lock while hashing or cloning a base, and adoption takes its exclusive counterpart. Clone subprocesses inherit the lock descriptor. The locks live under the selected Tart home, so processes using different Dockhand databases still coordinate access to the same external VM store. SQLite remains the authority for workflow records and provider capacity; these locks authorize no database change.

## Verification contract

Validation requires:

- a stopped source image and successful disposable boot;
- passwordless guest sudo;
- no active MacPorts ports;
- no recognized Homebrew, Fink, or pkgsrc installation path;
- the requested Darwin version and arm64 architecture reported by MacPorts;
- the requested MacPorts version and Tcl `json`, `json::write`, and `macports` packages;
- a selected compiler that can produce and run a C program; and
- the pinned Tart guest-agent version.

Verification and bump setup now choose the conventional native image when no explicit image is configured. A missing default reports `dockhand setup` as the next action. `--image` continues to select another prepared local image. Global `--tart` and `TART_BIN` route both setup and verification through the selected Tart executable.

## Validation performed

Focused package tests cover platform defaults, existing-image validation in a disposable clone, fresh provisioning order, preservation before adoption failure, candidate retention during failed adoption, missing-image check behavior, pinned asset verification, and shared/exclusive image locks. `go test ./...`, `go vet ./...`, and `make build` pass.

A real `dockhand setup --check` completed against the existing `dockhand-base-tahoe` image. The disposable clone reported Darwin 25 arm64, MacPorts 2.12.6, and guest agent 0.14.1-cb39b12, then was removed. This acceptance check exercised the native Tart listing, clone, boot, guest-agent, compiler, Tcl, pristine-image, shutdown, and cleanup paths.

The complete provisioning and recovery paths were then exercised with isolated image `dockhand-base-tahoe-smoke`. The default source resolved to the already-cached `ghcr.io/cirruslabs/macos-tahoe-vanilla@sha256:eeec54bfe1f076e27786c5d92b89187a05b1d109b5071eb2dcdf02d596e34640`. Setup cloned the vanilla source, configured and booted it, installed the pinned guest agent and MacPorts, validated the candidate, and created both the base and golden images. A second invocation with `--rebuild` successfully replaced the existing smoke base. After the base alone was deleted, ordinary setup restored it from the smoke golden and passed disposable-clone validation.

A fresh `verify hello --branch master --image dockhand-base-tahoe-smoke --wait` used an isolated SQLite database and had no reusable evidence. Job `job_QAO2IL3ZXYSPYL57FFYXQMUEJ6` was admitted, built `mail/hello` from tree `d4ac6a2e1d6e96e0a3cb5b0704b2c1802f694043`, recorded a passing attempt, and released its Tart resource. Both smoke images, the isolated database and artifacts, and the smoke image's zero-byte lock entries were removed afterward. Existing base, golden, source, and unrelated worker images were left in place.

The available Xcode archives were found under `~/Downloads/xcode_archives` rather than the initially stated hyphenated directory. The base profile intentionally did not consume them. Adding Xcode needs an explicit profile and recorded toolchain capability so a multi-gigabyte installation does not silently change the meaning of the default base image.

Dockhand v1's vanilla-source choice, base/golden naming convention, guest-agent bootstrap requirement, and pristine-image checks were consulted. The v2 package boundaries, implementation, comments, tests, and documentation were authored for this project; no v1 comments or tests were copied.
