# 2026-09-23: the forge contracts

The second of the 2026-09-23 [review](../reviews/2026-09-23-architecture-review.md)'s
contracts, after [`state.Scoped`](2026-09-23-state-scoped.md).

## What changed

`publish.Forge` was the one forge contract: nine methods, declared in
`publish`, a consumer, while `forge` held every value type the methods
use. The engine held no forge of its own and reached through
`e.Publisher.Forge` in thirteen places, most of them to observe or adopt
a pull request, which is not publishing. `app` did the same from
outside for the GitHub verification fork check.

`forge` now declares the two contracts a forge serves:

```go
type Accounts interface {
	Name() string
	Authenticate(context.Context) error
	AuthenticatedUser(context.Context) (string, error)
	NameFromRemote(string) (string, error)
	RepositoryInfo(context.Context, string) (RepositoryInfo, error)
}

type PullRequests interface {
	Find(context.Context, PullRequestQuery) (PullRequestObservation, error)
	Observe(context.Context, record.PullRequestRef) (PullRequestObservation, error)
	Create(context.Context, PullRequestInput) (PullRequestObservation, error)
	Update(context.Context, PullRequestInput) (PullRequestObservation, error)
}
```

`PullRequestInspector` stays optional and is asserted on the pull
requests. `publish.Forge` is gone: `publish.Service` takes `Accounts` and
`PullRequests`, the destination resolver, the remote selection, the
owned-fork check, and the preflight through the first and the
observation and write path through the second. The engine takes the
same two fields and its thirteen reaches are direct calls. `app` builds
the GitHub forge client once, wires it into the engine and the
publisher, and keeps it for the fork check, so nothing outside
`workflow` touches the publisher's insides. The engine also locks and
captures through its own repository rather than the publisher's, which
`app` wired to the same one; the two publication guards ask for a
publisher and a repository, and the publisher checks its own fields.

## The three choices, as made

- **Two fields, not one composed interface.** A consumer says which
  contract it uses, and the pull request lifecycle cluster, when it
  leaves the engine, takes exactly these two. The cost is that `app`
  assigns the same client four times.
- **`Authenticate` in `Accounts`.** The review's list left it out and
  only the preflight calls it. On GitHub it is the user lookup with the
  login dropped, but it says what a preflight means, and another forge
  may verify credentials without a user lookup.
- **The fork-ownership double stays.** The destination resolver checks
  ownership when it has a login; GitHub verification requires the login
  and a fork of upstream. They differ, and folding them is its own
  decision.

## Tests

The two fakes implement every method already and needed no change. Six
wiring sites set the two fields where they set one, one test asserts
the fake's type on the engine's pull requests rather than through the
publisher, and the continuation test that removed the publisher to get
"no forge is configured" now clears the two forge fields instead.
