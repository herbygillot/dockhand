# 2026-09-26: the Tart provider

Roadmap step 5, part 5's remaining piece: `check --on tart` builds in
dockhand's Tart images, over the guest channel step 3 built
([user doc](../tart-provider.md)). It is a v3 provider in
`internal/provider/tart`, beside the command and github providers, with
v2's Tart provider as its reference.

## What it does

- **One fresh clone per release and attempt**, of `dockhand-base-<release>`,
  run in the foreground and deleted afterwards, whatever happened. Each is
  named after its run, release, and attempt. A later attempt stops and
  deletes whatever an earlier one left when its driver died: the runner
  starts a new attempt only after settling the last, so those clones are
  known to be dead. The clone is reached over dockhand's SSH channel and
  held to its image's recorded host keys.
- **Room on the Mac.** macOS runs two VMs at most, the person's own among
  them, so an attempt waits while two are running and says so.
- **Staging.** The revision's tree, a port index for the release (the
  engine's own stager), the guest program, and its input are packed with
  v2's `staging.Archive` into one archive. It is copied in, checked by
  size and sha256, and unpacked.
- **The guest program** (`guest.tcl`) runs as root under launchd, so a
  dropped connection doesn't stop it, under the image's `port-tclsh`. It
  first checks its environment:
  - no ports installed;
  - no other package manager;
  - the staged tree indexed;
  - the platform the check asked for;

  and records the guest's macOS, tools, and MacPorts. Then, for each
  target in dependency order, as MacPorts CI does (decisions 11 and 22):
  1. everything is deactivated;
  2. the target is linted;
  3. its dependencies are installed and activated (`port install
     depof:`, from binary archives where they exist);
  4. it is fetched, checksummed, and installed with `-kn`, as CI's
     install-port is. A target with variants of its own installs with
     `-k` alone, so the dependencies its variants add come too;
  5. its declared tests run with a deadline, advisory unless the check
     requires them.

  A target whose changed dependency didn't pass is blocked, not built. A
  target an earlier attempt found blocked goes in marked so.
- **Results as they finish.** The guest rewrites `results.json` after each
  target. The provider reads it every ten seconds and records each new
  result with its log copied out, so a guest lost during its third target
  keeps the first two (decision 44). What the provider treats as trouble
  with the environment, which the runner tries again up to three times:
  - a guest that can't set up;
  - a program that stops without finishing;
  - a VM that stops;
  - results that can't be read for a minute.

  A build failure is a result, never retried.
- **Drift** (decision 10). When the guest's Command Line Tools differ from
  the facts table's row for its release, the check reports it, without
  changing any result.

## In the engine and the command line

- **Releases.** An optional `engine.ReleaseProvider` is a provider that
  takes releases, which Tart is. `--on tart:sonoma,tahoe` makes an
  environment of each. `tart` alone is this Mac's release, read from the
  kernel (`kern.osrelease`, in a `_darwin.go` file). `tart:all` is every
  release with an image, and a bare release name means Tart. A release
  without an image is refused before the check starts, naming the image.
  `Environments` takes a context now, to list the images.
- **The default.** A check with no `--on` and no `check.on` uses the
  command provider if one is set up, else Tart on this Mac's release.
- **Registration.** The provider is registered wherever Tart is on `PATH`.
  The command tests stand in for its lookup, so they don't depend on the
  machine they run on.
- **Settings.** `[providers.tart]` takes `capacity` (1 when unset) and
  `test_timeout`.
- **Describing a platform.** A platform is described by its macOS release,
  "tart macOS 26 (Tahoe) arm64", through `engine.DescribeEnvironment`,
  where the Darwin number showed before.

## Tests

- **The guest program**, run for real under `port-tclsh` with a stand-in
  `port` that records every command:
  - two targets in CI's order;
  - a failed dependency blocking its dependent;
  - tests advisory, required, and skipped;
  - a target blocked by an earlier attempt;
  - a guest that can't set up.

  It found a bug first: the test runner opened a pipe for reading while
  redirecting its output, which Tcl refuses. It now pumps the output into
  the log itself.
- **The provider**, on a fake Mac and guest:
  - results recorded as they appear, with their logs, and the clone
    deleted;
  - blocked targets marked;
  - an errored guest, an exited program, a protocol mismatch, and a
    stopped VM treated as infrastructure, with what was recorded kept;
  - an earlier attempt's clone swept, and a full Mac waited out;
  - cancellation;
  - drift;
  - the release selection.
- **The engine:** release environments, a bare release name, and the
  default.

## Proven on the Mac

- **The provider alone** (`TestLiveCheckInATahoeGuest`, run with
  `DOCKHAND_TEST_TART_LIVE`), in a real clone of `dockhand-base-tahoe`, in
  six minutes:
  - `tree`, with no dependencies, built;
  - `pv` built after its dependency;
  - pv's declared tests failed, and were rightly advisory;
  - no drift;
  - the clone deleted.
- **The whole path through the command line**, in a scratch clone and
  database: `init`, `start`, `revbump tree`, `check --plan` ("Provider tart
  macOS 26 (Tahoe) arm64"), `check` in the foreground, `status`, and
  `logs`.
  - The check passed for snapshot 1, and `status` showed it on Tahoe.
  - `logs` named the target's log.
  - The log shows the guest's sequence: the lint, then fetch and checksum,
    a look for a binary archive of the new revision, rightly not found,
    then the build from source, install, and activation.
  - The clone was deleted.

  A first attempt ran `init` and `start` on the real checkout, because the
  shell's `MACPORTS_TREE` pointed there. The branch and worktree it made,
  at master with nothing on them, were removed.

## Still to come

- **`dockhand providers setup tart <release>`**, over the existing
  provisioner, since v3 can't make images yet.
- **The Xcode image** for a port whose modelled `use_xcode` is yes
  (decisions 6 and 7).
- **Several releases in parallel**, where there is room.
- **Removing a clone left by a driver that died** when no later attempt
  comes, with `clean`.
- **Deleting v2's `internal/verify/tart`**, once its tests' rules are
  rewritten against v3 (step 4).
