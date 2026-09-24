# 2026-09-24: the guest channel, over Apple's ssh

Step 2 of the roadmap's Next, its first part; decisions 38, 42, and 43 of
the [contracts direction](../reviews/2026-09-23-contracts-direction.md),
with the choices the person settled for this step (the roadmap records
them). `internal/tart/channel` reaches a Tart guest over SSH through
`/usr/bin/ssh`, macOS's own client, which Local Network privacy does not
stop the way it stopped setup's Go dial from this session ("no route to
host" while Apple's `nc` connected).

- **Commands** (`Guest.Command`, `Script`) run with their arguments
  quoted for the guest's shell, over one connection per guest shared by
  every dockhand process (`ControlMaster`, a socket under
  `/tmp/dockhand-ssh-<uid>`, since the per-user temporary directory's
  path is too long for one). ssh reads none of the person's
  configuration (`-F /dev/null`). A command's own failure keeps its exit
  status; ssh's, 255, is `ErrTransport`.
- **Files** move by `ssh … cat`, the fastest mode the SSH tests found,
  and are checked by size and sha256 computed on the other side
  (`Upload`, `Download`, `Read`); a damaged transfer is tried three times
  and then reported as `ErrTransfer`, and a download leaves nothing
  behind unless it arrived whole. `Range` reads a log that may still be
  growing, checked against the same bytes hashed again on the guest. A
  missing file is `os.ErrNotExist`. SFTP is not used: it would run over
  the same connection, and nothing so far needs it.
- **Keys** (`Keys`, under `~/.dockhand/ssh`): dockhand's ed25519 pair,
  made with Apple's `ssh-keygen`, and the host keys recorded for each
  image. Every clone of an image presents the image's host keys, so a
  guest is trusted as the image it is named for (`HostKeyAlias`), never
  by its address. A bootstrap connection (`Bootstrap`) uses the image's
  `admin` password through `SSH_ASKPASS`, records the presented host
  keys, and installs dockhand's key (`InstallKey`); every later
  connection uses the key. The password stays in the image.
- `subprocess.Spec.StdoutOnly` streams a command's output to its writer
  without keeping a copy, so a transfer is not held in memory.
- `host.Machine.IP` is `tart ip --wait`.

Tests: unit tests against a stand-in `ssh` that runs the command locally
cover quoting, checked transfers, retry and refusal of damage, missing
files, a lost connection, the options for both credentials, and the keys.
`TestLiveChannel`, opt-in with `DOCKHAND_TEST_TART_IMAGE`, bootstraps a
clone, installs the key, moves 64 MiB each way intact, reads ranges, and
is refused by a guest named for another image's keys. It passes on
Monterey (12.7.6; 64 MiB up in 0.9 s, down in 0.4 s), Sonoma (14.8.7),
and Tahoe (26.6.2), the first two being releases whose `tart exec`
loses output.

Setup and verification move onto the channel in the next commits.
