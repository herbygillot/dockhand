# 2026-09-23: `state.Scoped`, the store bound once

The first of the 2026-09-23 [review](../reviews/2026-09-23-architecture-review.md)'s
contracts, taken as the binding alone: the records cluster stays in the
engine until the forge contracts show what the engine has left.

## What changed

`state.Store` was two things in one interface: a registry, three methods
that find, register, and list repositories by their Git common directory,
and two transaction entries that each took a repository ID. The reader
and transaction they hand out were already scoped, SQLite's transaction
carrying the ID and every statement filtering on it; what was wrong was
that the scope was chosen again at every call, from two fields that
never varied. `state.Scoped` is that choice made once:

```go
type Scoped interface {
	Repository() record.Repository
	View(context.Context, func(context.Context, Reader) error) error
	Update(context.Context, func(context.Context, Tx) error) error
}

func Bind(store Store, repository record.Repository) Scoped
```

The engine's `State` and `Repository` fields are one `State state.Scoped`,
and so are the retention collector's. `app` binds the store to the
registration it already held from its registry lookup, in the five
places it assembles an engine, and is now the only caller of the
registry. The verification providers keep their pool-scoped
`ProviderStore` and their repository ID, which names the executions they
write; that store was never repository-scoped and is not part of this.

## The three choices, as made

- **The registration, not the ID.** `Bind` takes the `record.Repository`
  rather than its ID, so the bound store knows the common directory it
  was registered for. The three places that re-read the registry every
  cycle, `requireRepository` and its two copies in preparation and
  integration, are one predicate, `boundRepository`, comparing the
  bound registration's directory with the driven repository's, with no
  database read. This is the one semantic change: a database swapped
  underneath a running engine is no longer noticed by that check. What
  the check exists for, an engine assembled against the wrong
  repository, is caught as before, and the tests that swap a foreign
  registration in still get their refusal.
- **An interface, so nothing is set for a dry run.** `Resolve` and a
  dry-run `Adopt` run with no store at all, and a nil interface stays
  the signal. It also lets a test wrap the bound store to count views or
  fail one write, which five tests do.
- **Stop at the binding.** Status, control, selection, submit, and
  scope still hang off the engine. They use only the bound store and the
  clock, and whether they become their own type, and behind what
  interface the CLI and `proc` would then see them, is a question the
  forge contracts will make easier to answer.

## What it removed

- Every state call in the engine and the collector carried the
  repository ID; none does.
- The guard `e == nil || e.State == nil || e.Repository == ""` lost its
  third clause at every site; the two guards that mean the store may be
  absent, in the resolution and adoption, are unchanged in meaning.
- Registry reads outside `app` and `state`: three per cycle, now none.
- The status result's repository ID comes from the binding.

## Tests

The workflow fixture keeps the registration beside the ID and hands out
`f.scoped()`, the store bound to it, for the tests that reopen the
database or wrap the store. The wrappers, an altered publication, a
failed release, a failed prune, a failed reuse, a failed publication, a
counted status, and a failed result, embed `state.Scoped` and override
one method without the repository argument. The perf tool binds its
stores the same way.
