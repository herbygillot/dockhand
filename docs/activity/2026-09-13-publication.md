# Standalone publication

Implemented `publish [--branch <branch>]` through the shared driver, with `--dry-run`, `--remote`, `--upstream`, `--base`, `--wait`, JSON results, and normal job-ID reattachment/cancellation.

## Behavior and organization

- `workflow` binds one tracked committed contribution, checks recorded verification with the existing applicability policy, atomically accepts immutable publication intent, and advances the existing job claim through push and PR checkpoints.
- `publish` owns planning, remote preconditions, PR content, and observation validation. The commit supplies the title and initial body; existing PR bodies remain unchanged.
- `forge/github` implements repository/fork discovery and PR lookup, observation, creation, and update. Shared HTTP handling now supports bounded JSON writes, distinguishes definitive rejections from unknown outcomes, and refuses write redirects. Authentication comes from configured credentials or GH_TOKEN/GITHUB_TOKEN and is never persisted.
- `git` inspects remotes, checks one-commit contribution history against the actual upstream base, and pushes one literal branch with an explicit expected head. Remote-tracking refs do not determine the push lease. Base fetching leaves tracking refs/FETCH_HEAD untouched. The existing descriptor-inheriting operation-lock mechanics also guard publication heads.
- `state/sqlite` migration 6 adds per-job publication intent/checkpoints, a head reservation spanning repository entries in one DB, and retained PR associations. Confirmation updates the action, job, PR, and published revision together. All state access remains repository scoped and no transaction spans external calls.

Push recovery can safely retry with its recorded expected value. PR recovery is deliberately different: after a write checkpoint, subsequent cycles only observe. Unknown HTTP outcomes are never blindly repeated. Cancellation preserves any pushed branch and still reconciles an issued PR request.

## Scope and limitations

The first CLI requires one tracked port directory, one contribution commit above the recorded base, and existing conclusive passing verification for the complete source tree and target. The latest terminal result supplies its recorded configuration. Missing verification scheduling, combined bump/publication, dependent cohorts, automatic rebase/squash, and later PR monitoring remain separate work.

An uncertain PR request can remain pending indefinitely, including a crash between checkpoint and send. Its head stays reserved because an absent PR does not prove a delayed request cannot create it. There is no new override/abandon command. GitHub metadata updates use observation-before-write and confirmation afterward; concurrent human edits in the intervening window cannot be fenced by the API. All cooperating local drivers must use the same DB; independent databases/hosts are outside this coordination scope.

## Authorship

All new implementation and tests were authored for v2. Existing v2 state, workflow, Git locking, applicability, and HTTP facilities were extended. No v1 comments or tests were copied, and no new dependency was added.

## Validation

Focused tests use local bare Git repositories, SQLite files, and HTTP fixtures. They cover literal expected-head pushes, no incidental tag pushes, upstream ancestry, real CLI planning/publication without Tart, confirmation and PR retention, lost responses, observation-only uncertainty, concurrent acceptance, repository isolation, stale source/remote/verification, and cancellation under the operation lock. GitHub tests cover request payloads, lookup ambiguity, closed/merged results, malformed observations, rejection classification, and redirect suppression.

Passed:

- `make build BINARY=/private/tmp/dockhand2-publication`
- `make test`
- `go test -race ./...`
- `go test -race ./internal/workflow -run 'Publication|PublishCLI' -count=1` after the final cancellation-order adjustment
- `make vet`
- `git diff --check`

The built executable is `/private/tmp/dockhand2-publication`; the existing ignored `dh2` binary was preserved. No live branch was pushed and no real PR was opened.
