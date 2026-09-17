# Remotes are recognized, not named

Every publication so far needed `--remote herby` because the push remote defaulted to `origin`, which in this checkout is the upstream repository. The ownership guard refused correctly, but the user was being asked for a fact the checkout already contained.

## Behavior

`publish.Service` now carries the upstream repository name (`macports/macports-ports` in the application) and resolves remotes from their URLs:

- The upstream is any remote whose URL names that repository, whatever it is called. `--upstream` overrides; a remote literally called `upstream` and then the fork's parent remain the fallbacks.
- The fork is the remote pushing to a repository the authenticated login owns, excluding the upstream. Without a login, the only non-upstream remote is taken, and publication still checks ownership once it authenticates.
- `--remote` is an override rather than a default. It is required only when the choice is ambiguous, and the error then names the candidates: two owned forks, or several non-upstream remotes while logged out, in which case the message points at `dockhand auth login`.

GitHub verification resolves its destination through the same path, so `--provider github` no longer needs `--remote` either. The CLI flag defaults changed from `origin` to automatic.

## Validation

Publish tests cover automatic selection with the upstream called `origin`, ownership deciding between two non-upstream remotes, the logged-out ambiguity message, an explicit remote planned without a login, and the unchanged refusals for an explicit remote the user does not own or that does not exist.
