# 2026-10-05: the full stage's setup, from its first use

The Prime-time thread set the full stage up on the M1 for v0.3.0-rc1 and worked around four gaps by hand. Each is in the harness alone; the candidate stays 6fec2ac8.

## What changed

- **reset-user.sh doesn't stop at the login.** It waited at step 2 for a person to log dhtest in, exiting 75, so steps 3 to 5 never ran, and a rerun deleted dhtest again. Nothing after it needs the login: Git's identity, the test key, and the fork's clearing run as dhtest without it. The login is now said last, as `THEN:`, and the rest runs.
- **reset-user.sh clones dhtest's ports tree.** Nothing made `MACPORTS_TREE`, `~/Source/macports-ports`, which A2 expects to be dhtest's fresh fork clone. It now clones the test account's fork there, read over HTTPS and pushed to over SSH, with MacPorts' own as `upstream`, borrowing the host mirror's objects while it clones. `full.sh` stops where `MACPORTS_TREE` isn't a clone, naming reset-user.sh.
- **full.sh puts MacPorts on PATH.** dhtest's login shell lacked `/opt/local/bin`; full.sh prepends it and `/opt/local/sbin` where it's missing.
- **The fork's sync is checked.** Clearing the fork needs the test token's Contents and Pull requests write access on it; without them the fork's master had drifted 107 commits behind MacPorts'. After the sync, reset-user.sh asks GitHub how far behind it still is, and stops, naming the permissions, where it isn't level. The README says so.

## From the rc1 full stage's first rows

- **H5 isn't judged before dockhand is installed.** In the full stage A0 runs before A1 installs the dockhand under test, so H5's `dockhand --json status` failed with no error, and A0 graded a blocker though `make test` passed. With no dockhand at `DH_BIN`, H5 is "skipped", saying so.
- **A row's WAITING reaches the run's own output.** A checkpoint's `WAITING:` went to the row's `runner.log` and `$ACCEPT_STATE/waiting` alone, since the row's output is its log, and A1 sat two hours unseen. run.sh opens fd 3 on its own output, and a checkpoint writes there too; the README says where it appears.

## A10's configuration, and what A1 checks

- **The full stage names dockhand's configuration file.** A10's setup read `DOCKHAND_CONFIG`, which only the quick stage set, and died before its act ("parameter null or not set"). full.sh now exports it as dhtest's own, `~/.dockhand/config.toml`, where dockhand reads it by default, and makes it if it isn't there. A10 and E10, which change it for their run (a maintainer, and a command provider), keep it as it was and restore it in teardown, since dhtest's outlasts the row; A10 replaces a maintainer line rather than adding a second.
- **A1 checks the version, which is what a tagged build names.** dockhand @0.3.0-rc1 printed "dockhand v0.3.0-rc1" with no commit, where A1's header said it names the version and commit. A release tarball has no Git data, so a build of it records no revision; the Portfile's ldflag sets the version, and a tag names its commit, which `buildinfo.Source` finds a Generated-By trailer's build by. The header was wrong, not the Portfile, and now says so.

## H4 and A2

- **H4 leaves Tart's images and guest-built archives out.** It found a token's shape in three disk.img files, the Golden Gate vanilla image and two clones of it, before any token was on the host: gigabytes of binary make the pattern turn up by chance. Nothing of the host's login goes into a guest or its archives, since dockhand gives a guest the revision, its targets, and kept archives, never a credential. So H4 searches dockhand's state without its `tart` and `archives` directories, `.img`, or `.tbz2` files: its database, configuration, and logs, and the logs and results copied out of guests, as before. Matching the token's value instead would need the harness to hold the test login, which it never sees.
- **A2 runs `auth login`.** Its checkpoint asked for a code that nothing had printed: `setup`, without a terminal, says to run `auth login` rather than running it. A2 now runs `auth login --no-browser` in the background, waits for its one-time code, and checkpoints with the code and address in the WAITING line, then waits for the login to finish.
