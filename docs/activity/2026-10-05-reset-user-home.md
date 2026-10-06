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

## 2026-10-06: two phases again

createhomedir froze the M1 at dhtest's first login, after a full reset at 04:35Z had made and filled the home before macOS's first-login setup (inferred; the flow without it worked). `--run` now deletes and makes dhtest and stops with WAITING to log it in once; `--run --after-login` does the per-user steps in the home that login made. `--make-home` keeps createhomedir as an explicit opt-in, and naming it with `--after-login` is refused.

## 2026-10-06: A10 on a fresh account

A10's setup wrote the maintainer line with `grep -v`, which exits 1 for a config with no other line, as dhtest's empty one, so the copy was skipped and the row recorded its setup failed. It filters with `sed` now.

## 2026-10-06: S1 runs alone, for a day, on the real workload

S1 said it ran last but ran in glob order, before S2, so its soak held up the end of the run. A row marked `# order: alone` now runs only when named, and run.sh says to run it with `--rows S1` after. S1 soaked an idle serve: a fresh account has no maintainer and cleanup's defaults never act on a host with room. It now sets the person's maintainer line (ACCEPT_MAINTAINER), `serve.for_outdated = "check"`, and `cleanup.after = "12h"`, restoring the config in teardown, and samples serve's memory, free disk, plain and compressed logs, and auth status every hour for 24 hours, the person's choice (ACCEPT_S1_HOURS), then lists Tart's VMs for the judgment.

## 2026-10-06: A4's findings

- **H3 says when it couldn't count.** dhtest's gh was never logged in, the snapshot swallowed `gh pr list`'s failure, and H3 passed while A4 opened herbyg-test/macports-ports#1. A failed listing is kept in `prs.err`, and H3 reads "not checked" with the reason.
- **full.sh stops unless gh is the test account.** H3 and every row's `close_test_pr` need it; it checks `gh api user` against ACCEPT_GH_LOGIN before any row.
- **reset-user.sh logs dhtest's gh in** with the test account's token (GH_TOKEN, the token file, or the driver's gh), in gh's own file rather than a Keychain, and stops if the token is another account's.
- **A4 allows its own push and pull request.** The person's submit pushed `dockhand/<port>` to the fork, which H2 called harm.
- **README and providers.** setup offers the maintainers line only as the ports your GitHub login maintains write it, and otherwise says how to write it; update changes the branch's files without a preview, which `dockhand diff` shows, and the steps after it preview; a release with only its Xcode image is said to need its plain image beside it, which a check does since batch 100, rather than "which most ports build in", since a check builds in the Xcode image where there is one (the person's decision of 2026-09-26).
