# Retention and local verification smoke exercise

Exercised the rebuilt CLI after `9b8808d`, using a separate database at `/tmp/dockhand-retention-smoke/state.db` and dedicated `dockhand-smoke-tahoe` images. The ports checkout remained on committed `master` at `87ff2b89b11d1666d39a51929499c5787bebecdf` (tree `d7768980df40c495e24db97a2240149fc94b9d6a`). No source edits or PR were needed for this verification/retention exercise.

## Provisioning

- `dockhand setup --os tahoe --image dockhand-smoke-tahoe` provisioned a fresh CLT image from the cached vanilla image, including guest agent and MacPorts installation. Guest diagnostics reported macOS 26.6.2, Darwin 25 arm64, CLT 27.0, and MacPorts Base 2.12.6; host Tart was 2.37.0.
- `setup --check --os tahoe --image dockhand-xcode-tahoe --xcode ~/Downloads/xcode_archives/Xcode_26.6_Apple_silicon.xip` passed using a disposable check clone. An initial check against the 26.3 archive correctly rejected the existing image's 26.6 installation; this was an exercise input mismatch, not a provisioning defect.
- Deleted the dedicated smoke base/golden images after verification and cleanup. Standard user images were preserved. This was a targeted CLT provisioning and Xcode-check exercise, not another full OS/Xcode provisioning matrix.

## Verification and cache lifecycle

Ran:

```sh
./dockhand -T ~/Source/macports-ports \
  --db /tmp/dockhand-retention-smoke/state.db \
  verify jq --branch master --image dockhand-smoke-tahoe --fresh

./dockhand -T ~/Source/macports-ports \
  --db /tmp/dockhand-retention-smoke/state.db \
  wait job_KKBZREI6GXXW2EIAH4Z25HRKLZ --trace
```

The first run generated its full PortIndex in approximately 3 minutes 12 seconds. Source preparation happened before VM capacity reservation: the provider execution occupied count was zero during indexing. Both a concurrent `gc --older-than 0 --dry-run` and an actual collection left the active work alone. The initial command exited after admission; a separate `wait` invocation attached to the continuing build. Lint, build, declared tests, and install all passed, and the VM was released.

Repeated the same fresh verification with `--trace`. The second run reused the cached image identity and PortIndex without regeneration. Concurrent garbage collection successfully removed the now-disposable host index while the VM continued using its staged copy. Index locks remained in place.

The second run passed build but failed native `port -d test`. MacPorts requires destroot before tests; the jq destroot log shows a root-privileged recompilation of `src/main.o`, and inspection of the retained VM confirmed that object was owned by root, mode 0644. The subsequent unprivileged test build tried to overwrite it and failed with `Operation not permitted`. Repeating `sudo /opt/local/bin/port -N -D /var/tmp/dockhand2/ports/sysutils/jq -d test subport=jq` directly in the retained VM reproduced the failure. Repeated generation of `src/version.h` appears to trigger the recompilation; the reason the first fresh run passed remains unproven. No ownership repair, test skipping, or change to verification semantics was made to obtain a pass.

Dockhand recorded the failed verdict and retained the VM for investigation. Verification used native MacPorts steps; no executable version-output comparison was used. The VM was stopped after inspection and subsequently released by gc.

## Cleanup defect and correction

Live collection exposed a composition regression: maintenance registered GitHub in `Engine.Providers` but Tart only in the legacy single `Engine.Provider` field. Once a provider map exists, lookup uses it exclusively, so Tart artifact cleanup reported that pruning was unsupported.

Registered Tart in the same provider map. Added an app integration regression that creates a released Tart execution and real diagnostic directory, previews collection, removes it through the actual app composition, and confirms the database retains the passed verdict and records the prune marker. The test uses a nonexistent Tart executable to establish that pruning already-released diagnostics does not boot or require a VM.

After rebuilding, actual collection released the failed VM and removed the successful run's old artifacts. A second collection removed the newly released run's artifacts; a third returned no items. Before/after snapshots showed identical jobs (including attempts and evidence), changes, revisions, and PR records. Both resources remained recorded as released with artifact-pruned timestamps.

The real default database was previewed only. GitHub offline log-cache retention was exercised through the automated integration tests; this pass did not create a new remote CI run or PR.

## Evidence and validation

- First job: `job_KKBZREI6GXXW2EIAH4Z25HRKLZ`, attempt `attempt_7RXHOKWPRJ6JCXCBHS5CHN6ZS7`: passed.
- Second job: `job_KQSCJ74IJ3EOF2PCZ6GZOGHL7D`, attempt `attempt_OIPWPXIAKK4TX66ILA5Y6JEAAV`: failed native jq tests.
- Local exercise logs, retained SQLite evidence, and collection snapshots: `/tmp/dockhand-retention-smoke`. These temporary files are not repository fixtures and may be removed by the host later.
- `go test -race ./internal/app`, `go test ./...`, and `go vet ./...` passed after adding the regression. The rebuilt binary completed the live cleanup checks.

Authored the composition fix, app regression, and this report. No v1 code, comments, or tests were copied.
