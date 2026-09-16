# Publication: refuse pushing to a repository the user does not own

The new-user deno exercise ran `publish --dry-run` in a checkout whose `origin` is `git@github.com:macports/macports-ports.git` and whose fork is a remote named `herby`. The plan proposed pushing the contribution branch to `macports/macports-ports` and opening a same-repository pull request. The README promises that nothing is pushed to the MacPorts repository directly; destination resolution only selected the push remote by name, and the GitHub verification provider was the sole place that checked fork ownership.

## Change

`publish.Forge` gains `AuthenticatedUser`, which the GitHub client already provided. `Service.Destination` now requires the push repository's owner to equal the authenticated login, case-insensitively, before resolving the base repository. The refusal is a `publish.ErrPrecondition` naming the remote, the repository, and the login. It lists the other local remotes whose push URL parses to a repository the login owns, without network calls, so the fix is one `--remote` flag away; when none exists it says to add one. `Service.PlanTo` repeats the ownership check on the frozen destination so a destination recorded earlier cannot be published by a different login.

The check applies wherever destinations are resolved: standalone `publish`, `bump --publish` and `bump-revision --publish` intake, corrections, and GitHub-provider verification. `publish --dry-run` resolves the same destination, so it now needs a GitHub credential; an anonymous dry run previously rendered a plan that an authenticated publication could refuse. The GitHub verification provider keeps its parent check and no longer needs its own owner comparison, which remains harmless.

No new dependency or package. The workflow test forge reports a fixed login so existing publication fixtures, which publish to a repository owned by that login, are unchanged.

## Checks

- New tests in `internal/publish/plan_test.go`: refusal with the fork hint when `origin` is upstream; refusal of another contributor's fork; success from the owned fork with the parent as base; the no-fork hint; and `PlanTo` refusing a frozen destination for a different login.
- `go test ./internal/publish/ ./internal/workflow/... ./internal/app/... ./internal/cli/... ./internal/verify/github/...` and `go vet`: passed. The publish CLI dry-run test now supplies its token before the dry run and removes it to exercise the authentication refusal.
- Real tree exercise with the rebuilt binary against a verified deno revision-bump contribution:
  - `publish deno --dry-run` (origin = macports): refused, `select your fork with --remote: herby (herbygillot/macports-ports)`.
  - `publish deno --dry-run --remote pguyot`: refused, another contributor's fork.
  - `publish deno --dry-run --remote herby`: plan rendered `herbygillot/macports-ports:dockhand/revbump/... -> macports/macports-ports:master`. No job accepted, nothing pushed.

## Docs

README, `cli-design.md`, `usage.md`, and `github-verification.md` state the ownership rule. The roadmap was reprioritized: this item, PortIndex consolidation per `portindex.md`, and first-use path hardening lead; Cargo `rev=` support, platform-coverage gaps, and PR observation follow unchanged.
