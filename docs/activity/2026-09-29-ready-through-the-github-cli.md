# 2026-09-29: a refused ready goes through the GitHub CLI

The person decided D8 on 2026-09-29. When an organization refuses dockhand's app the change that takes a draft out of draft, as the macports organization's OAuth App access restrictions do, `submit --ready` runs `gh pr ready` itself. It does so where the GitHub CLI is installed and signed in as the account dockhand is, and says so: "Marked #35011 ready for review with the GitHub CLI: the macports organization refuses dockhand's app."

Why dockhand's app is refused: the macports organization restricts which OAuth apps may act for its members, and hasn't approved dockhand's. Nothing on the app itself changes that; only the organization's owners can, when a member asks from GitHub's settings (Settings → Applications → Authorized OAuth Apps → dockhand → Request access). The GitHub CLI's app is one the organization accepts.

- **Telling the refusal apart.** GitHub says it only in words: its GraphQL error names the organization's "OAuth App access restrictions". The GitHub client recognizes that text, the one the sshuttle run saw, and reports it as `forge.ErrAppRestricted`. That's a dependence on GitHub's wording, not on a documented code, which GitHub doesn't give; other refusals are passed on as they were.
- **The same account.** The GitHub CLI is used only when it's signed in as the account dockhand acts for, matched without regard to case, as GitHub's logins are. Otherwise the pull request would be changed as someone else, or refused. Whatever the reason the CLI wasn't used, it's said after the instructions: not installed, signed in as another account, or refused too.
- **gh's login stays gh's.** `github.CLI` runs only documented commands, `gh api --hostname github.com user --jq .login` and `gh pr ready <n> --repo <repository>`, and never reads gh's token. It sits in `internal/github`, beside the credential lookup that already asks `gh auth token` when dockhand has no login of its own. The engine takes it as `GitHubCLI`, which the command layer sets, and the command tests set a stand-in, so no test runs the GitHub CLI a Mac has.
- **The journal** says "marked #N ready for review with the GitHub CLI".

The guide says how to ask an organization's owners to approve dockhand's app, and the architecture note where `github.CLI` fits.

Tests:
- `TestAReadyTheOrganizationRefusesGoesThroughTheGitHubCLI`, in the engine: no CLI, then one signed in as dockhand is, and the journal;
- the command's submit test: no CLI installed, another account's, the same account in another case, and a refusal that isn't the organization's;
- `TestTheGitHubCLIRunsItsDocumentedCommands`, against a scripted `gh`, and `TestMarkReadyTellsAnOrganizationsRefusalOfTheApp`, against a fake GitHub.

Thirteen mutations each fail a test.
