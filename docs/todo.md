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

## A stale primary silently enlarges a hand-made cohort

**Found in the live check (2026-09-03).** A branch cut from
`origin/master` picks up every upstream commit the local primary branch
has not caught up to, and dockhand counts those commits' portdirs as
part of what the branch changes. It then builds them.

Observed: a branch holding one no-op edit to `devel/oniguruma6` and one
to `sysutils/jq` was submitted as
`verify: submitted oniguruma6, jq, mise on Tahoe`. The third member came
from `9515815e0ba mise: update to 2026.9.1` — dockhand's own pull request
#34485, which merged two steps earlier in the same session.

**Why it happens, and why neither half is wrong on its own.**
`ChangedPortdirs` diffs from `MergeBase(PrimaryBranch, tip)`, and
`PrimaryBranch` is documented to never fetch: "the local position is the
answer, staleness included" (D21, `internal/git/git.go`). That is a
deliberate and defensible choice — it is what `git diff master...HEAD`
would say too. Separately, `status` runs a retire sweep that advances
`origin/master` when one of your PRs merges. Each is reasonable; together
they mean dockhand can move the remote ref underneath you and then
attribute the commits it fetched to the next branch you cut from it.

**Branches dockhand mints itself are immune.** `planOnBase` forks from
`primary`, so the merge base is exactly the fork point. Only hand-made
branches taken from a remote-tracking ref are affected — which is an
entirely ordinary thing to do, and what this document used to instruct.

**Consequences, worst first.** A promoted branch's PR body would claim to
change a port the author never touched. A cohort builds ports nobody
asked for, spending guest time and a licence slot. And the ABI cohort
would measure a stranger's port as though it were part of the change.

**Possible answers, none ruled.**

1. Say something. dockhand knows `primary` and can see whether
   `origin/<primary>` is ahead of it without a fetch. When it is, and the
   branch's roster includes portdirs from commits the branch does not
   own, one advisory line naming them. Cheap, no refusal, no behaviour
   change.
2. Base on the remote-tracking ref instead. Matches what GitHub will diff
   the PR against, and contradicts D21's "never fetches" premise.
3. Count only commits the branch itself introduces — the roster is the
   union of portdirs touched by `primary..tip` rather than the diff of
   the endpoints. Narrower and more literal, but it changes what "what
   this branch changes" means for a branch that legitimately carries a
   merge.

(1) is the smallest honest step and does not disturb D21. Worth pairing
with the `status` sweep, since that is what moves the ref.

**A related facet, same root.** `discard`'s advisory calls whatever
`TrackedRemote` returns "the fork". In a checkout where `origin` is the
canonical repository and the fork is another remote, a hand-made branch
tracking `origin` produces `the fork copy on "origin" is untouched` —
naming the upstream as the fork. `promote` resolves the fork properly,
by matching the authenticated `gh` login against each remote's owner
(`gh.ForkRemote`); the advisory should use the same answer rather than
assuming the tracked remote is the fork.

## `status` should only report; a `cycle` verb should do the work

**The ask (maintainer, 2026-09-04).** Reverse `status`'s mandate so it
is purely read-only, and introduce `dockhand cycle` to carry the pump
and the flush.

**What `status` does today, beyond reporting.** `statusAction` calls
`Reconcile` with `Drain: true`, and that pass has four phases, three of
which write:

1. **Settle.** `inspect` polls each branch's job and, when one has
   finished, writes its verdict into the note and releases the worker.
2. **Retire.** A branch whose pull request merged is deleted — locally
   and on the fork. The report says so as it happens: `removed
   dockhand/edit-2.0.0 from "herby"`, `discarded dockhand/edit-2.0.0`.
3. **The publish slot.** `publishPass` runs the machine's publication
   road, gated by `GateRing3`.
4. **Drain.** `PumpDeferred` submits what was queued, which boots VMs.

So a command named `status` can delete a local branch, delete a remote
branch, write notes, release and boot virtual machines, and — on a build
where the road is enabled — publish. Observed during the live check:
`status` deleted two merged branches mid-run, and separately advanced
`origin/master` underneath a checkout, which is what caused the stale
primary finding above.

**Why this is worth doing.** The name is a promise and it is not being
kept: `status` is the one verb a person types to find out where things
stand, including when they are unsure what state the tree is in — which
is exactly when they least want it acting. It is also the verb a script
or a watch loop reaches for, and there is currently no way to ask "what
is the state" without also asking "and change it".

**Shape.** `Reconcile` already separates the phases behind `ReconcileOpts`
(`RetireOnly` is `clean`'s shape, `NoClean` withholds the deletion,
`Drain` gates the pump), so the split is mostly a matter of which verb
sets which flag rather than new machinery. `cycle` would be
settle + retire + publish slot + drain; `status` would be the observation
and the rendering alone.

**Where the line falls (maintainer, 2026-09-04).** `status` **keeps
settling**. Reconciling what the workers did into the ledger is reading
the world and writing down what it said, and a report that left a
finished job reading `verifying` would be a worse lie than the write it
avoided. What moves to `cycle` is anything that acts on the world:

- deleting branches and pull requests (retire, and `clean`'s sweep)
- submitting work and starting workers (the drain)
- publishing a pull request (the publish slot)

So "read-only" here means **makes no change anybody else can see** — not
"writes no bytes". The ledger is dockhand's own account of what it
observed; a branch, a PR and a VM are the world.

**A boundary this ruling settles by implication, worth stating.**
Settling *releases* the worker of a finished run, and releasing destroys
a VM. That stays with `status`: the line drawn is at *starting* work,
and a released environment is the last step of the verdict being written
rather than an action of its own. The failure path already keeps its
environment deliberately, so nothing a person still needs is taken away.

**`status` names the remedy (maintainer, 2026-09-04).** Where work is
waiting, the report says so and names `dockhand cycle`. With the split
nothing begins on its own, and a queued run that nobody is told about is
a run that never starts. This is the same remedy-beside-the-finding
shape the rest of the output already uses, so it is wording rather than
machinery — but it is load-bearing wording, and its absence would be the
whole feature's failure mode.

**`clean` folds into `cycle` (maintainer, 2026-09-04)**, with a `--keep`
flag: cycle inbound and outbound, and remove nothing.

**What `--keep` should cover.** Today's deletions and releases, so that
one flag means "act on the world, but take nothing away":

1. **Merged-PR branches** — retire's deletion, local and the fork copy.
   The named case, and today's `status --no-clean`.
2. **Superseded branches** — `clean --superseded`. Already conservative
   by ruling: a supersede is dockhand's own inference, so nothing removes
   one without being asked. `--keep` should keep it that way rather than
   quietly inheriting a sweep.
3. **The environment of a passing run.** Settle releases it; a failure
   keeps it deliberately. Under `--keep` a green run could hold its guest
   too, which is what a person wants when the question is "it passed, but
   what did it actually install?" Weigh against capacity: a kept
   environment is a spent slot, and two of those is the whole licence.
4. **Untracked workers.** `status` reports orphans and removes nothing.
   If `cycle` ever reclaims them, that is a deletion and belongs behind
   the same flag.

**One flag per concept, not a universal `--keep` (maintainer,
2026-09-04).** A single flag over all of these would be hard to aim: a
person withholding one deletion would withhold three, and would have to
know which three.

There is a second reason the universal flag cannot work, which the list
above makes visible only once it is written out — **these do not share a
default.** Removing a merged PR's branch happens today unless asked
otherwise; removing a superseded branch happens only when asked, by
ruling, because a supersede is dockhand's own inference. So a uniform
`--keep-x` family would be withholding for one and meaningless for the
other. The vocabulary has to follow the default:

- **on by default, `--keep-…` withholds it** — merged-PR branches
  (`--keep-merged`), which is today's `status --no-clean` under a name
  that says what it keeps.
- **off by default, a plain `--…` asks for it** — superseded branches
  (`--superseded`, the flag `clean` already carries), and reclaiming
  untracked workers if `cycle` ever does that at all.

**A case that does not fit either, and needs its own answer.** Keeping
the environment of a *passing* run cannot be a `cycle` flag, because
releasing it is part of settling and settling stays with `status`. Three
ways out, none ruled:

1. `status --keep-environments`, a withholding flag on the verb that is
   otherwise pure observation. Honest, and slightly against the grain of
   what `status` is becoming.
2. Split settling: `status` writes the verdict, `cycle` releases the
   guest. Clean on paper; it means a green run holds a slot until
   somebody cycles, and two held slots is the whole licence.
3. Leave it out. A person who needs to look inside a green build has
   `verify --trace`, and the failure path already keeps its environment.

(3) is the cheapest and may be right — the want is real but rare, and
the other two both cost something structural.

**Still open.**

- Exit codes: `status` today can exit on a band a write caused. A
  read-only verb's bands are its own question.
- The goldens move: every `status` golden that shows a retirement line
  belongs to `cycle` afterwards.
- `status --no-clean` disappears with the split — the flag exists to
  withhold a deletion `status` will no longer do. Whether it is dropped
  or kept as an accepted no-op is a compatibility question, and this is
  pre-release, so probably dropped.

**To discuss before implementing.** The maintainer has asked to take
this one up as a conversation rather than a ticket.
