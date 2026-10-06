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

## 2026-10-06: A5 and A7

- **A5 copies a WAL database with its -wal and -shm,** and backs up from the copy: a read-only open of dhtest's database failed with "unable to open database file", having no -shm and no leave to make one. A database already at the candidate's schema, with no earlier copy, is now "not run", naming ACCEPT_REAL_DB, rather than a known issue.
- **A7 says what to stage,** a dockhand newer than the candidate, and to answer skip when there's none; its teardown uninstalls serve's agent, which a skipped A7 left running through the rows after.

## 2026-10-06: each row selects its own branch

The rc5 and rc6 full stages' rows left branches open, and later rows pick theirs by port: after section A, go-reflex had three open (A10's examples twice, and B1's), so B1's `check -p go-reflex` was rightly refused as ambiguous, and B4's `status --port … branches[0]` took A10's. Before each live full-stage row, run.sh now archives the open branches earlier rows left (`isolate_branches`, logged in the row's isolation.log), keeping, and saying on the run's output, any whose pull request is still open, for the rows that ask a person to name one. A row's own branch is `own_branch`: one open now that wasn't when it started. B2's second check takes its bump's branch that way rather than the first `dockhand/*` ref, which was A12's, and B2 names its pull request (`# prs:`) and allows its push and pull request, which H2 and H3 would have called harm.

## 2026-10-06: B4's second check, and a current sandbox

- **B4 tidies its answer to the review before it checks it.** It checked a branch with commits and an edit on top, without a terminal, which asks whether to build the commits or the files and refused; the tidy after it left a commit no check had passed, so the second submit stopped and the row never reached what it tests. No other row checks edits on top of commits. The README's "committed or not" now says a check with edits on top of commits asks which, and `--head` or `--working-tree` answers without a terminal.
- **submit_pr syncs the sandbox's master with MacPorts' before a test pull request,** so it shows only its own commits; reset-user.sh synced it only at the reset.

## 2026-10-06: B6 makes its own merge; the sandbox is reset outright

- **B6 makes and merges its own pull request in setup,** a test one in the sandbox, merged with gh, before the harm sweep's snapshot: a person made one inside the snapshot's window, and H2 and H3 took the pull request and the merge for the row's harm. It's not run unless the approval list has its test pull request.
- **The sandbox's master is set to MacPorts' outright,** by its ref, in submit_pr, B6, and reset-user.sh: `gh repo sync --force` leaves a master that is only ahead of MacPorts', as B6's merge left it. submit_pr allows the move, which H2 would otherwise call a push. reset-user.sh now stops on a master ahead of MacPorts', not only one behind.
- **B7 uninstalls serve's agent in its teardown,** skipped or not. It and S1 each install the account's one serve agent, so they can't run at once.

## 2026-10-06: B9 keeps its check.on to itself

B9 appended `[check] on = ["github"]` to dhtest's config and left it, so every later row's check would have run on GitHub Actions. Its setup now keeps the config and its teardown puts it back, as A10's do. Its "no Tart set up" couldn't hold in run order, A4 and A12 having made images, so it gives dockhand an empty Tart home of its own, `DOCKHAND_TART_HOME`, which dockhand reads (internal/tart). It takes its branch with own_branch.

## 2026-10-06: C4 and C7 make what they test

- **C4 plans pomo against the quick stage's pin.** The full stage's master is MacPorts' own, where pomo has moved on, so its update had nothing to change and the module-move case never ran. In the full stage the row now makes a bare master of its own at the pin, borrowing the ports clone's objects as quick.sh does, and names it with `DOCKHAND_UPSTREAM` for the row alone.
- **C7 makes its branches in setup,** before the harm sweep's snapshot, as B6 does: two test pull requests merged in the sandbox, the second's fork branch moved by a commit pushed after its merge, a third closed with an uncommitted edit in its worktree, and a branch archived, listed in the row's fixtures file. Its teardown sets the sandbox's master back to MacPorts'.

## 2026-10-06: rows that submit name their branch

With earlier rows' branches set aside, a `submit --plan` on the ports checkout's master is refused for want of a branch before it reaches what the row asks about. D-C1 (a login) and F3 (the fork's remotes) now make and tidy a branch of their own in setup, logged in, and submit it with `-b`. D-T3 fails only on a `dockhand/*` branch it started, not on those earlier rows left. No other row runs submit, check, or tidy without naming a branch, but C8's, which runs in a branch's worktree.

## 2026-10-06: logins without waiting codes

Two D-C2 device codes expired while the run waited for a person. Rows that log out to test what follows, D-C1, D-C2, and D-C5, now keep dockhand's Keychain login in a shell variable, never a file or a log, and put it back as it was through `security`'s standard input (`login_keep`, `login_restore`), so no code is asked for; D-C5 asks for the GitHub CLI's login only where reset-user.sh's isn't the test account's. Where a code is needed, A2's first login and D-C3's after its revocation, `device_login` asks the person to say they're ready before setup github issues one. D-C3, which left the login revoked for the rows after, now logs in again beside the revocation. The rows a person does on GitHub's site, D-C3 and D-C4, run next to each other, after D-C1 and D-C2, which now need no one.

## 2026-10-06: D-C2 follows a pull request through the logout

D-C2 asked serve to say once that it couldn't act after a logout. It can: a running serve keeps the client it made, and its token, which GitHub honours until it expires, and reads public pull requests without a login past that; the rc6 run's serve, with nothing to follow, said nothing. The row now opens a test pull request in setup, closes it while dockhand is logged out, and passes when serve says it closed, with the same process before and after the login comes back. That serve doesn't say it's logged out is a README known issue, with the fix under Later.
