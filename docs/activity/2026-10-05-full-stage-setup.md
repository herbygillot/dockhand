# 2026-10-05: the full stage's setup, from its first use

The Prime-time thread set the full stage up on the M1 for v0.3.0-rc1 and worked around four gaps by hand. Each is in the harness alone; the candidate stays 6fec2ac8.

## What changed

- **reset-user.sh doesn't stop at the login.** It waited at step 2 for a person to log dhtest in, exiting 75, so steps 3 to 5 never ran, and a rerun deleted dhtest again. Nothing after it needs the login: Git's identity, the test key, and the fork's clearing run as dhtest without it. The login is now said last, as `THEN:`, and the rest runs.
- **reset-user.sh clones dhtest's ports tree.** Nothing made `MACPORTS_TREE`, `~/Source/macports-ports`, which A2 expects to be dhtest's fresh fork clone. It now clones the test account's fork there, read over HTTPS and pushed to over SSH, with MacPorts' own as `upstream`, borrowing the host mirror's objects while it clones. `full.sh` stops where `MACPORTS_TREE` isn't a clone, naming reset-user.sh.
- **full.sh puts MacPorts on PATH.** dhtest's login shell lacked `/opt/local/bin`; full.sh prepends it and `/opt/local/sbin` where it's missing.
- **The fork's sync is checked.** Clearing the fork needs the test token's Contents and Pull requests write access on it; without them the fork's master had drifted 107 commits behind MacPorts'. After the sync, reset-user.sh asks GitHub how far behind it still is, and stops, naming the permissions, where it isn't level. The README says so.
