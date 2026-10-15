# submit and setup name the GitHub account; auth goes (v0.4.0 part 1, c; D-C4)

The rc9 full stage's D-C4: with another account's token in GH_TOKEN,
`submit --plan` previewed "From herbyg-test/macports-ports:dockhand/…" and
named no account. With it, the command package split's 7a row 2 and 7b
row 3's `SetupReport`, and `auth`'s deletion, as the split's coalescing
placed them (93ba18b6).

- **`github.Client.LoginStatus`**: the account GitHub says the token is,
  where the token comes from (`CredentialSource`, with `Words()`: GH_TOKEN,
  "dockhand's login in the Keychain", "the GitHub CLI's login"), and, for
  dockhand's own login, until when it renews itself. `forge.Login` carries
  it to the engine, through `Forge.Login` (forge/github's client;
  forgetest's ada, from GH_TOKEN).
- **submit's preview names the account**: "As       ada, from GH_TOKEN",
  beneath From, from `SubmitPlan.Login`, which `destination` reads in
  place of `AuthenticatedUser`. Where the fork's owner check gives way,
  under a sandbox or `--remote`, a fork another account owns is said
  (`SubmitPlan.ForkOwner`): "! herbyg-test/macports-ports is herbyg-test's,
  and this pushes and writes the pull request as herbygillot". A warning
  for now; whether it holds or refuses is the person's to decide. The JSON
  carries `account`, `login_source`, and `fork_owner`.
- **setup names the login, asked of GitHub**, where it said only where a
  token was: "✓ ada, from dockhand's login in the Keychain, renewing itself
  until …". The GitHub CLI's login, which submit takes where nothing else
  is set, is named too; setup said "not set up" of it. A login GitHub
  can't be asked about is said with why.
- **`setup --json`** (`setupReport`, the review's SetupReport): the
  checkout, upstream, Git, worktrees, MacPorts, the providers' lines, the
  GitHub login (`account`, `source`, `renews_until`, or `problem`), and the
  records. No legacy count: bridge 1 goes in hygiene. A `--json` setup asks
  and offers nothing.
- **`auth` deleted**, its login, status, and logout. Acceptance rows
  re-pointed: A2, B7, C1, F3, F5, F10, S1, and D-C4 run `setup` (S1 reads
  `setup --json`'s login a line an hour); D-C1, D-C2, and D-C5 log out with
  `setup github --logout`; D-C1 asserts submit names `setup github`, where
  it matched the GitHub CLI's "gh auth login"; D-C5 asserts setup's
  `github.source` is the CLI's. The docs that named `auth login` name
  `setup github`, and README's v0.3.0 known issue, whose workaround was
  `auth status`, goes.
- The command tests no longer reach this Mac's login: `authStore` is a
  memory store and `authAPI` finds no credentials unless a test gives them
  (`withAuth`), since setup now asks GitHub.

Tests: TestALoginStatusNamesTheAccountAndWhereItsTokenComesFrom,
TestASubmissionToAnotherAccountsForkIsSaid,
TestSubmitPreviewsThenOpensThePullRequest (the As line, the JSON),
TestSetupGitHubLogsInAndOutAndSetupNamesTheLogin (was
TestAuthLoginStatusAndLogout: setup's line, setup --json, GH_TOKEN's
precedence), TestALoginThatCantRenewIsSaid.
