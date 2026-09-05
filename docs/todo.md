# TODO

Ideas worth doing that are not being done yet, with enough context to
act on later without rediscovering why. Unlike `decisions.md`, nothing
here is ruled; unlike `notes.md`, this is maintained. An entry leaves
when it ships or when it is decided against — and if it is decided
against, it moves to the decision log rather than simply vanishing.

## Promote: read the other PR's version from its Portfile, not only its slug

**What remains of the open-PR version check (shipped 2026-09-05 as
`b8cb4e8`).** Where another open PR's head branch is one dockhand
minted, the same-port note now says whether it takes the port to the
same version, newer or older, under `macports.VerCmp`, and names the
slug as its source. A hand-made head branch establishes nothing and
the generic note stands. The dearer source the original entry named —
the Portfile in the PR's head tree, read the way dockhand reads any
Portfile — is deferred: it answers for every PR rather than only
dockhand's own, at the cost of a fetch per candidate.

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

## `discard` calls the tracked remote "the fork"

**What remains of "a tracked upstream is not a push" (shipped 2026-09-05
as `0647471`).** Retire and discard now read the remote-tracking ref,
so a branch that was never pushed is never called promoted and no `gh`
call is spent learning it. One facet is still open: the discard
advisory names the remote it found as "the fork". In a checkout where
`origin` is the canonical repository and the fork is another remote,
that word is wrong whenever the copy lives on `origin`. `promote`
resolves the fork properly, by matching the `gh` login against each
remote's owner (`gh.ForkRemote`); the advisory should say "the copy on
<remote>" and leave "fork" to the verb that knows.

## The stale-primary advisory is not yet paired with the sweep that moves the ref

**What remains of "a stale primary silently enlarges a hand-made
cohort" (shipped 2026-09-05 as `2232b9a`).** `ChangedPortdirs` now
names, on stderr, each roster member that came from a commit the
branch does not own, with the commit and the remedy. Still open: the
condition is created by the retire sweep advancing `origin/<primary>`
under a checkout whose local primary stands still, and the sweep says
nothing when it does. A line there — "origin/master moved past your
master by N commit(s); a branch cut from it will carry them" — would
put the warning at the cause rather than only at the symptom.

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

**`clean` folds into `cycle` (maintainer, 2026-09-04, confirmed).**
`cycle` cleans a branch locally and off the fork when its upstream PR
merged, and `--keep-<x>` flags withhold each specific thing it would
otherwise remove.

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

**The passing run's environment: decided at submit, not at settle
(maintainer, 2026-09-04).** All three options above were the wrong
frame. By the moment `status` settles a green run the release happens in
the same pass, so no flag on `status` or `cycle` could intervene, and
the person who wants to look inside a green build knows it when they
start the run. So: `--keep-env` on `verify` and on the `bump` family,
recorded on the run the way `Test` and `Trace` are, honoured wherever
release happens. The failure path's keep-by-rule gets a sibling,
keep-by-request. `cycle` never touches passing environments.

**Settle stays in `status` (maintainer, 2026-09-04, reconsidered and
confirmed).** Settle is the one write that makes `status` truthful:
every other write in `Reconcile` changes the world, settle changes the
report to match a world that already changed. Moving it out would make
`status` show "verifying" over a guest that finished an hour ago until
somebody ran `cycle`, in the commonest loop the tool has, and would
hold green guests' slots on a two-slot licence. The pure read exists
for the cases that want it — a watch loop that must not take locks, a
script that wants the notes and only the notes — as **`status
--no-update`**: show the ledger as written, poll nothing, write
nothing.

**`cycle --reclaim-orphans` (maintainer, 2026-09-04).** Untracked
workers — VMs no note claims — are reported by `status` and removed by
nobody. `cycle` may remove them, opt-in, following the vocabulary rule:
this is new, it destroys environments nobody has characterised, so it
is a plain flag that asks for it and not a default with a keep.

**Design complete (2026-09-04); what remains is the work.**

- `status`: observe, settle, render. Names `cycle` where work is waiting.
  `--no-update` is the pure read. `--no-clean` is dropped — it withheld a
  deletion `status` no longer does; pre-release, no shim.
- `cycle`: retire merged-PR branches locally and off the fork
  (`--keep-merged` withholds), `--superseded` as `clean` had it,
  `--reclaim-orphans`, the drain, the publish slot. `clean` is retired.
- `verify` / `bump` family: `--keep-env`, on the run.
- Exit codes: `status` keeps the observation bands; a band a write
  caused belongs to `cycle` now.
- Goldens: every `status` golden showing a retirement or a drain line
  moves to `cycle`.

Discussed as asked; recorded as D27.

## A cohort stops at the first failure, including for members that do not depend on it

**Observed live (2026-09-03), raised by the maintainer (2026-09-04).**
The cohort runner breaks out of its loop the moment a member fails:

```sh
else echo failed > "$d/.state.$i" && mv -f "$d/.state.$i" "$d/state.$i"
     ok=no
     break
```

Everything behind that member is never attempted, and settle records it
`blocked` on the sibling. In part C of the live check, `mise` — which
has no relationship to `oniguruma6` at all, and was in the cohort only
because of the stale-primary bug — came back "oniguruma6 fails to build;
this member is untested".

**The information to do better is already in hand.** `Dependent.Requires`
carries each member's own dependency targets, and `dependencyOrder`
topologically sorts the members with it. What is missing is that the
guest is never told: the runner is a shell loop over indices with no
notion of which member needs which.

**Shape.** Stage a `requires.<i>` beside each member's `argv.<i>`,
holding the indices that member depends on. The runner attempts every
member, skipping one whose prerequisite has `state.<j>` of `failed`, and
records why. A member skipped for a failed prerequisite is `blocked` and
blamed on it, exactly as now; a member with no failed prerequisite is
built, where today it is abandoned.

**Why it matters more since the cap came off.** With the cap at eight, a
first-member failure wasted at most seven builds. With the cap off, a
cohort can hold every dependent a library has — `libffi` has 132 — and
one early failure abandons all of them. Combined with the dependents
being best effort, those abandoned members settle terminal and do not
block the promotion, so the change publishes with many revision bumps
resting on builds nobody ran. That is honest, because each is named, but
it is a great deal of honesty about a great deal of nothing.

**Cost, stated plainly.** This is not a small change. It moves the
frozen runner bytes (`guest_test.go` pins them), the staging that writes
the argv files, and the blame logic in `verdict/cohort.go`, whose
`Stopper` and `Culprit` are built on there being exactly one member that
stopped the run. The cohort corpus's `.expect` files encode the current
shape and would move with it.

**Ruled (maintainer, 2026-09-04), so this can now be designed.** A
member whose prerequisite failed is `blocked`: something the change is
responsible for did fail, and this member is untested because of it —
and the sibling sentence, "X fails to build; this member is untested",
becomes true rather than borrowed, because X really is its
prerequisite. `withheld` stays narrow, for a member nothing was wrong
with. And the judge may trust the guest's per-member `state.<i>` files,
which is the carrier that tells "skipped for a failed prerequisite" from
"never reached": a Portfile forging its own cohort's state would be a
maintainer deceiving their own tool about their own bump, which is not
worth engineering against. Recorded as D25.

## Let a person force a withheld member to build

**The ask (maintainer, 2026-09-04), tentative.** A withheld member is
owed nothing — it is bumped, the person is told it was not built, and
that is the whole of the tool's obligation. But a person who wants it
built anyway should be able to say so: force the withheld member into
the guest, deactivating whatever it conflicts with first.

**Shape.** A flag on `bump-revision --for` — `--force-withheld`, or
`--build=gegl-devel` naming the member — that seats the member instead
of holding it back. The runner then needs one new step before that
member's install: `port -f deactivate <the conflicting sibling>`, so
MacPorts will activate it. The sibling's own verdict stands as already
measured; what its deactivation costs is that any member built *after*
it, which depends on it, now binds the archive's copy rather than the
cohort's — so a forced member should be built last, after every member
that might need the sibling active.

**What it is not.** Not the default, and not a scheduled follow-up.
The ruling is that the tool informs and stops; this is the person
overriding, and the flag should read that way.

**In scope for the last phase (maintainer, 2026-09-04), built last:**
it depends on the runner change (the forced member must build after
every member that might need the sibling active), and on nothing else
depending on it.

## The patches-unchecked finding reaches the plan, not yet the pull request body

**What remains of "a bump that fetches nothing leaves its patches
unchecked, and says nothing" (shipped 2026-09-05 as `19f90cc`).** The
plan now carries a `patches-unchecked` finding, Accepted rather than
Proposed so it is a statement in the ABI check's "unavailable" shape
and never holds the machine gate, and `--plan` prints it. It does not
yet reach the pull request body the way `abi-unavailable` does; a
reviewer of a port whose patches went unchecked should read that where
the verification is vouched for.
