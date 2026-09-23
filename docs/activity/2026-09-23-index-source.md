# 2026-09-23: the index source

The last of the 2026-09-23 [review](../reviews/2026-09-23-architecture-review.md)'s
four contracts, after [`state.Scoped`](2026-09-23-state-scoped.md), the
[forge contracts](2026-09-23-forge-contracts.md), and the
[verification ledger](2026-09-23-verification-ledger.md).

## What changed

`portindex.Stage` followed by `portindex.Open` was written in five
places: the selection reader `app` builds, with a map remembering what
it had staged; dependent discovery; the survey twice, once best-effort
to group explicit names by Portfile and once to select by filter; and
Tart's source staging, which then opened the index once per target to
check it. Each spelled the same thing: install the tree's index into
the tree's root, then read it from there.

`portindex.Source` is that once:

```go
type Source interface {
	Index(context.Context, macports.Tree) (*Index, error)
}
```

`Stager` is the source that stages. It installs a tree's index into the
tree's root on first sight, with its configuration resolving the
indexer and its repository reading the sources, opens it from there
after, and remembers what it staged, so one command resolving names
repeatedly against one tree indexes it once. A tree that names no
platform is indexed for the native one, which the selection reader's
stager is given. `WithoutBase` builds a tree's index from the nearest
cached generation rather than its recorded base: name lookup takes it,
since it needs no generation of master to resolve a name, and
verification and discovery keep the base so a candidate's index derives
from it. `SourceFunc` lets a test hand a fixture index in.

The selection reader's `Index` is a `Source` rather than a function;
`app`'s reader is a stager, and the staged-index map it carried, a
roadmap item since 2026-09-16, is the stager's memory. Dependent
discovery, the survey, and Tart's staging request take a `Source` in
place of an index configuration; the survey's two stagings are one
call each, and the survey tolerates no source for explicit names, as it
tolerated an unusable configuration, while a filter requires one. Tart
still derives the index recipe from the build's frozen configuration
through `tart.SourceIndex`, and `app` wraps it in a stager for
discovery: the recipe is Tart's knowledge, since it is Tart's
configuration that freezes the indexer and its digest.

## Tests

The stager has its own test against the real indexer: a tree is staged
once and only opened after, a second stager over the same cache finds
the generation, a tree naming no platform and a stager with no
repository are refused. The selection tests pass their fixture through
`SourceFunc`, dependent discovery's test builds a stager, and the
staging test opens the index it checks targets against.
