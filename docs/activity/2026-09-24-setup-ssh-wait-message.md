# 2026-09-24: setup says why it stopped waiting for SSH

A scratch `dockhand setup --os tahoe --image dockhand-scratch-tahoe`,
run to prove the Tart and tools changes end to end, stopped after four
minutes at "Installing the Tart guest agent..." with nothing but
"context deadline exceeded"; its cleanup stopped and deleted the guest.
`waitSSH` (`tart/provision/ssh.go`) now says the guest at its address did
not accept SSH within four minutes, with the last attempt's error, and
that a guest that is up may be unreachable because the app running
dockhand lacks macOS's Local Network permission, which blocks the Go dial
setup still uses (step 2 moves setup to `/usr/bin/ssh`). A cancellation by
the caller is still reported as that. The cause of this run is
investigated separately.
