# Provisioning reliability exercise

Rebuilt Dockhand and exercised fresh provisioning, independent image validation, repeated agent registration, failed replacement, extraction failure, cancellation, and a real MacPorts build/test/install. Existing conventional images and source archives were preserved. Investigation images used distinct names and at most two VMs ran simultaneously.

## Findings and changes

- SSH's configured timeout did not apply to the manually established handshake. Dial and handshake are now bounded; cancellation closes the connection before session creation as well as during execution/upload. SSH readiness has an overall deadline. A silent local SSH endpoint regression covers timeout, cancellation, and connection teardown.
- Guest-agent readiness now has a five-minute bound and reports its last probe. Registration observes the launchd domain and service, waits for unavailable domains or exit 125, accepts a concurrently loaded service only after observing it, and preserves other bootstrap failures. Initial new-helper testing found zsh's reserved `status` variable; it was renamed and the script regression now uses the actual guest login shell. Fresh provisioning and repeated live registration passed afterward. The earlier exit 125 itself did not reproduce, so this is tested handling of the plausible readiness conditions, not proof of its historical cause.
- Command Line Tools output is streamed. Long storage preparation, uploads, and Xcode installation emit elapsed-time progress through a synchronized writer. A failed Xcode install retains its workspace until the disposable VM is removed, avoiding slow recursive cleanup inside that doomed guest. A shell regression preserves the original extraction failure and workspace; a CLI-injected extraction failure returned normally and removed its candidate.
- Replacement previously deleted the old image before cloning its successor. It now retains `<image>-previous` until cloning succeeds and rolls back after failure or cancellation. Failed rollback preserves both the previous image and validated candidate. Interrupted replacement with a missing destination is reported explicitly rather than hidden by golden-image restoration. The destination lock is still inherited by the cloning process.

All new helpers and tests were authored for v2. Existing operations were adapted in place; no v1 comments or tests were copied. No new dependency, package, database schema, or command was added. macOS installation behavior remains in `macos`; Tart image lifecycle, transport, and progress remain in `tart/provision`.

## Monterey/Xcode investigation

The original input archive is unchanged: `Xcode_14.2.xip`, SHA-256 `686b9d53ca49e50d563bc0104b1e8b4f7ccfe80064a6d689965fb819bf8efe72`. The source remains `macos-monterey-vanilla` digest `4bc904cf8f2447dfa6e6fb9a5eb70eba14196dd0f4236d30fc92aff9c2391842`.

Native extraction succeeded in two fresh Monterey 12.7.6 guests: first with the initial binary, then with the reliability changes. The guest had four CPUs and 8 GiB RAM; sampled open-file usage was well below its limit. A copy also expanded successfully with the host's native xip. Guest logs, process/resource observations, and archive identity were retained. These observations establish that the archive and native extraction path can work; they do not identify the cause of the two earlier failures. No alternate extractor, archive rewrite, memory increase, or unproven workaround was introduced.

The second successful setup created the previously missing conventional `dockhand-xcode-monterey` and `dockhand-golden-xcode-monterey`. An independent disposable-clone check passed. Another clone compiled a local C fixture through `port -d build`, ran `port -d test`, completed `port -d install`, and executed the installed program successfully. That clone was deleted; the prepared image remains pristine. No executable version string was used as evidence of source identity.

## Live results

| macOS | CLT check | Xcode check |
| --- | --- | --- |
| Monterey | Passed | 14.2: passed after two fresh provisions |
| Ventura | Passed | 15.2: passed |
| Sonoma | Passed | 16.2: passed |
| Sequoia | Passed | 26.3: passed |
| Tahoe | Passed | 26.6: passed |

Each check used `dockhand setup --os <release> --check --json`, adding `--xcode ~/Downloads/xcode_archives` for the Xcode profile. The checks cover transport, passwordless sudo, absence of foreign prefixes/active ports, MacPorts/Tcl, compiler execution, platform identity, and developer-tool selection. Existing vanilla images already supplied usable CLT installations; this pass does not certify every softwareupdate download scenario.

Fresh Sonoma and Sequoia CLT provisioning also passed. Re-registering both guest services twice on the owned Sonoma guest passed. An injected Sequoia adoption-clone failure restored the old image and retained the validated candidate; the restored image passed an independent check, followed by a successful ordinary rebuild. Ctrl-C during Sonoma validation exited 130, removed its temporary clone, and preserved the original. Injected xip failure during a rebuild preserved the existing investigation image and removed the failed candidate.

All investigation images, smoke-test clones, and the temporary host extraction were removed. The final local inventory has twenty conventional ready/golden images, covering all ten profiles, with no running VM or temporary next/check/previous image. The six original Xcode archives and cached OCI sources remain.

## Validation and evidence

`go test ./...`, `go vet ./...`, focused race tests for provisioning/macOS, and `git diff --check` passed. Rollback tests were strengthened afterward and passed separately under race detection. Built using `/opt/local/bin/gmake build` because Apple's make launcher is blocked by the pending host Xcode license; the host license/installation was not changed.

Raw logs, input hash, matrix command/result records, failure-injection runners, and inventories are in `/tmp/dockhand-provision-reliability`. That directory is temporary; the conclusions and key evidence are retained here. The opt-in `DOCKHAND_TEST_BOOTSTRAP_VM` test requires an owned running disposable VM and is skipped in the ordinary suite.

The earlier Monterey extraction and Sonoma/Sequoia exit 125 causes remain unestablished. Preserve the new stage-specific diagnostics if either recurs; do not infer a root cause from successful retries or call the historical failures proven fixed.
