# TODO

Ideas worth doing that are not being done yet, with enough context to
act on later without rediscovering why. Unlike `decisions.md`, nothing
here is ruled; unlike `notes.md`, this is maintained. An entry leaves
when it ships or when it is decided against — and if it is decided
against, it moves to the decision log rather than simply vanishing.

## Promote: say when an open PR already takes this port as far or further

**The ask (maintainer, 2026-09-03).** When promoting, look for an open
pull request on the same port that updates it to the same version or a
newer one, and tell the user. Explicitly **not a hard gate** — a flag,
not a refusal.

**What exists today.** `promote` already searches the open pull
requests on the port (`gh.OpenPortPRs`, now off the search quota and on
paged REST). `verdict.CheckDuplicates` then does two things with the
result:

- An **exact title match**, case- and whitespace-insensitive, is a
  refusal: `DuplicatePRError`, exit 20, overridable with
  `--no-pr-check`.
- Every **other** open PR on the port produces one advisory note:
  *"an open PR already touches this port: #N …"*.

**The gap.** That note is a bare fact about coexistence. It does not
say whether the other PR is doing the same work, less work, or more.
The cases it currently under-serves:

- Someone else has an open PR taking the port to a *newer* version
  than the one being promoted. The promotion is not a duplicate and is
  not wrong, but it is probably wasted, and the user would want to know
  before spending a reviewer's attention.
- Someone else has an open PR at the *same* version whose title is
  worded differently — "bump to 1.9" against "update to 1.9". The exact
  title match does not catch it, so it reads as an unrelated PR.

**The hard part, and why this is not a two-line change.** Getting a
version out of another PR honestly. A title is freeform prose and
parsing one is a guess; a wrong guess here is worse than silence,
because the whole point is to tell the user something true about
someone else's work. Two sounder sources, in order of cost:

1. The PR's changed files — the port's Portfile in its head tree — read
   the way dockhand reads any Portfile. Correct, and expensive.
2. The PR's head branch name, when it was minted by dockhand, since the
   slug carries the version. Free and reliable for dockhand-minted
   branches, useless for hand-made ones.

Whatever the source, the comparison is `macports.VerCmp` and never
string ordering, and the sentence must say where the version came from
so a reader can weigh it. If no version can be established, the honest
output is today's generic note — the feature declines to guess, like
everything else here.

**Where it goes.** `verdict.CheckDuplicates` is the pure judgment and
already returns notes plus an optional refusal; this is a richer note,
not a new gate, so its shape does not change. The version facts would
have to arrive on `PRFact`, gathered at the engine boundary where the
rest of the GitHub mapping happens, so `verdict` stays pure.

**Not to be confused with** the same-port supersede rule, which is
about *this* checkout's own sibling branches and is already ruled and
built. This one is about other people's work.

## `verify` promises a view that cannot show the branch

**Found in the live check (2026-09-03).** `verify` on a branch outside the
`dockhand/` namespace succeeds, mints a schema-3 note, and closes with
*"`dockhand status` follows it"* — but `status` never shows it.
`Engine.Resolve` accepts any branch outright
(`internal/engine/resolve.go`), while `Engine.Reconcile` enumerates
`repo.Branches(ctx, git.BranchNamespace)` and nothing else. So the note
exists, the worker runs, and the view our own epilogue points at is
structurally incapable of mentioning either.

Observed on a hand-made `erasure-test` branch: verification submitted and
ran correctly, note written at the branch tip, and `status` listed the
sixteen `dockhand/*` branches without it.

**Three ways out, in ascending order of how much they change.**

1. Say less: drop the `status` clause from the verify epilogue when the
   resolved branch is outside the namespace. Honest, one render change,
   and it leaves the user with no way to follow the job they just started.
2. Refuse: `verify` only accepts namespaced branches. Simple, and it
   removes an escape hatch that costs nothing and is genuinely handy.
3. Show it: `Reconcile` also lists non-namespaced branches that carry a
   verify note. The ledger is keyed by commit, not by branch name, so the
   note is already findable; the namespace walk is the only thing hiding
   it. This is the one that matches how custody actually works — a note
   that no view surfaces is a gap in the board, not a branch that deserves
   silence.

Leaning (3), which is why this is a TODO and not a one-line fix: it widens
what `status` walks, and that touches the retire sweep, the forge lookups
per branch, and every status golden. Worth doing deliberately.

## A tracked upstream is not a push

**Found in the live check (2026-09-03), pre-existing — not an overhaul
regression.** `Engine.retire` decides a branch was promoted by asking
whether it has a tracked remote at all:

```go
if repo.TrackedRemote(ctx, b.Branch) == "" {
    return          // never pushed
}
b.Retire.Promoted = true
```

`TrackedRemote` reads `branch.<name>.remote` (`internal/git/remote.go`).
That key answers "where does this branch's upstream live", not "has this
branch been pushed under its own name" — and `git switch -c foo
origin/master`, the ordinary way to start a branch, sets it to `origin`
for a branch that exists nowhere but locally.

Observed on `dockhand/live-solo-control`: upstream
`refs/remotes/origin/master`, zero matching heads on `origin`, and
`status` reporting `promoted; no PR found`.

**Three symptoms, one cause.**

1. `status` calls a never-pushed branch promoted.
2. It spends a `gh` lookup per such branch — exactly the cost the gate's
   own comment says it exists to avoid.
3. `discard` (`internal/engine/discard.go:73`, same test) advises
   `git push origin --delete <branch>` for a fork copy that does not
   exist. Following that advice fails.

**Note that `TrackedRemote` is not the defect.** Its doc comment —
"names the remote a branch tracks" — is exactly what it does. Both call
sites ask it a question it does not answer. The fix is a new predicate,
not a change to what `TrackedRemote` means.

**Ruled (maintainer, 2026-09-03): use the remote-tracking ref.** A branch
counts as pushed when `refs/remotes/<remote>/<branch>` exists locally.
Three candidates were weighed:

- `branch.<name>.merge == refs/heads/<name>`. Free and local, but a
  branch pushed with a bare `git push origin X` sets no `merge` key at
  all and would read as never pushed.
- **The remote-tracking ref — chosen.** Also free and local, and it
  catches both `push -u` and bare `push`, since git writes the ref on any
  successful push. It is already how dockhand reasons about pushes
  elsewhere: `PushForce` uses `--force-with-lease` because, in its own
  words, the lease is the remote-tracking ref the last push recorded.
  Same source of truth, applied consistently. Its weakness is staleness —
  a remote copy deleted elsewhere leaves the ref until `fetch --prune` —
  which for status's purpose is the right answer anyway, because the PR
  did exist.
- `git ls-remote --heads`. Ground truth, and wrong here: a network call
  per branch to avoid a `gh` call per branch is strictly worse than what
  it replaces.

**Implementation note.** `Reconcile` already enumerates branches, so it
can resolve the whole set with one `for-each-ref refs/remotes/<remote>/`
rather than a call per branch — cheaper than today's code, not merely
more correct. Both `retire` and `discard` move to the new predicate.

**Deferred until the cohort live check completes** (maintainer's call,
same ruling). It moves goldens in `status` and `discard`, and changing
rendering midway through a check that compares real output against
stated expectations would muddy every remaining part. Nothing downstream
depends on fixing it first; part F is unaffected, since that branch is
genuinely pushed. `docs/cohort-live-check.md` annotates the false line as
expected wherever it appears.
