# 2026-09-24: Tart through its official interfaces

Step 1 of the roadmap's Next, the Tart item: decisions 31, 32, and 38 of
the [contracts direction](../reviews/2026-09-23-contracts-direction.md).
Dockhand now learns what exists and what runs only from `tart list` and
`tart get`, and never reads or writes inside a Tart home except the one
flagged exception, the recovery-partition edit.

## What Tart says, and what dockhand makes of it

`tart.Client.Run` names the three failures dockhand acts on, from Tart
2.37.0's own words (`internal/tart/images.go`):

- `ErrListingBlocked`: `"image info --plist …" failed … Resource
  temporarily unavailable`, which `tart list`, and `tart get` of the VM,
  print for as long as a VM with an ASIF disk runs (openai/tart#1344).
- `ErrVMMissing`: "does not exist", except from `delete`, which says it
  of a running VM it leaves in place (openai/tart#1345).
- `ErrVMStopped`: `stop`'s "is not running".

The listing and the new `Client.Get` read pointer fields and refuse an
entry that lacks the ones dockhand reads (`Name`, `Source`, `Running` or
`State`; `DiskFormat`), so a Tart that renames them fails loudly rather
than reporting every VM stopped.

## State, deletes, and names (`internal/tart/host`)

- `LocalVM` no longer looks for `vms/<name>` when the listing does not
  name the VM. A clone Tart killed midway leaves nothing, or a
  temporary directory Tart's next command removes.
- `Delete` deletes only a VM the listing shows stopped and succeeds only
  when the listing no longer names it, whatever `tart delete` said; a
  failed delete whose VM is gone succeeds, and one whose VM is still
  listed fails with Tart's words attached.
- `Clone` refuses a listed destination, as before, and so do setup's
  own clones (`provision.native.Clone`, the adoption clone), since
  `tart clone` onto a stopped VM replaces it silently. The new `Rename`
  requires a stopped, listed source and an unlisted target, since Tart
  renames running VMs; setup's renames use it.
- `Machine.Blocked`, when set, waits out a blocked listing; setup sets it
  (every 15 seconds, saying so once). Without it the error returns and
  the caller waits: a verification submission reports at-capacity with
  the reason, and the provider's capability check counts a blocked
  listing as a working Tart.

## Setup's guest (`internal/tart/host/foreground.go`, `provision`)

`StartForeground` runs `tart run` in its own process group and returns a
`Foreground` handle, so the terminal's Ctrl-C reaches dockhand, which
stops the guest itself. `Foreground.Stop` runs `tart stop`, then SIGINT,
then SIGKILL to the group, waiting between each; "not running" is no
failure. Setup's `Stop` uses the handle and never lists first. Its
cleanup, on failure and after `--check`, stops and then deletes even when
the stop failed, and joins what went wrong into the command's error,
naming `tart delete <name>` for a VM it could not remove. An interrupted
`setup --check` on a real Monterey image stopped and deleted its clone
and left no `tart run` behind.

## ASIF declined

Setup reads the fresh clone's `DiskFormat` before anything runs it and
declines anything but raw, naming openai/tart#1344; its cleanup deletes
the clone. Verification declines an ASIF image the same way before
cloning it (`verify.ErrImageUnavailable`), for an image made some other
way. Golden Gate's images are ASIF, so Golden Gate is declined until Tart
fixes the listing.

## Storage (decision 38)

`Configure` now frees the recovery partition on every image, not only
Xcode images, so the guest agent can grow the container over the whole
100 GB disk; it edits `disk.img` only after `tart get` says raw. The
agent's daemon, which does the resize, now logs to
`/var/log/tart-guest-daemon.log` in the guest; its failures were silent.
Both take effect on images provisioned from now on; the Tahoe rebuild
queued with the tools pin will carry them.

## Locks

Image and setup locks moved out of `<TART_HOME>/dockhand/locks` into
`~/.dockhand/tart-locks/<digest of the canonical Tart home>/`: per user,
so every database sharing a Tart home shares them, and outside the Tart
home. An older dockhand running at the same time uses the old location,
so do not run one alongside this one; `~/.tart/dockhand` can be removed
once none does. Test binaries that take these locks point HOME at a
temporary directory (`testsupport.IsolateHome`, and
`IsolateHomeKeepingTart` where opt-in live tests need the person's
Tart home).

## Pinned against the real Tart

Unit tests pin the parsed shapes and messages. Two opt-in tests in
`internal/tart/host/live_test.go` pin them against the installed Tart,
cloning and never changing the named image:
`DOCKHAND_TEST_TART_IMAGE=dockhand-base-monterey` covers the fields,
`stop`'s "not running", a refused clone onto a listed name, and
`delete`'s "does not exist" for a running VM it leaves (#1345);
`DOCKHAND_TEST_TART_ASIF_SOURCE=ghcr.io/cirruslabs/macos-golden-gate-vanilla:latest`
covers the blocked listing (#1344). Both pass on Tart 2.37.0.

Found while writing the second: a `tart list` issued while an ASIF VM is
starting makes that start fail with `VZErrorDomain Code=1` "The virtual
machine failed to start": ten of eleven starts listed within their first
seconds failed, and all nine left alone for 15 seconds or more, by hand or
in the test, succeeded. The
listing reads every VM's disk, the ASIF path through `diskutil image
info`. Dockhand never starts an ASIF VM, but until it has its own Tart
home (step 2), its listings can break a person's own Golden Gate VM as
it starts. Recorded in the direction record; not filed upstream.
