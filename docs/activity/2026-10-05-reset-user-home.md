# 2026-10-05: reset-user makes dhtest's home, and checks the token first

From the Prime-time thread: `reset-user.sh dhtest --run`, run on the test host at 22:05Z, deleted and made dhtest, then stopped at its first per-user step. Harness only (tools/acceptance).

## What changed

- **Per-user steps run in dhtest's home.** `sudo -u dhtest` kept the driver's HOME, so `git config --global` tried to lock the driver's `~/.gitconfig`. Every per-user step is now `sudo -H -u dhtest` (`as_dhtest`).
- **dhtest's home is made with it.** sysadminctl only assigns the home, which macOS makes at first login; `createhomedir -c -u dhtest` makes it from the user template. Where it still isn't there, the run stops with WAITING to log dhtest in once and rerun with `--after-login`, which keeps dhtest and does the steps after making it.
- **The fork's token is checked before anything is deleted.** GH_TOKEN, else `~/.dockhand-acceptance/gh-token` (ACCEPT_GH_TOKEN_FILE), else gh's own login; a run stops up front when there's none, or when it can't write to the fork. A dry run says it would stop.
- **The token's write is tried, not read.** The repository's `permissions.push` is the account's role, which a fine-grained token without Contents write still reads as true; the dry run passed and the sync then failed with 403 (merge-upstream). A real run now makes a branch in the fork and deletes it before anything else, and a dry run says it would. The messages ask for Workflows write too, since a sync that brings MacPorts' workflow changes needs it.
- **D-I5 says the driver unlocks FileVault.** sysadminctl warns a user made with `-password -` can't unlock it, so the reboot checkpoint has the admin driver unlock, then log dhtest in.

## Verification

- Dry runs against a scratch host root, with and without `--after-login`, and the harness selftest.
