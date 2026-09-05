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

**Ruled (maintainer, 2026-09-04): option 1.** An advisory line naming
the portdirs the branch does not own, emitted where the roster is
derived. No refusal, no change to what the branch is taken to change,
and D21 stands. Worth pairing with the sweep that moves the ref, since
that is where the condition is created.

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

## A failed member says nothing for itself in the body's member list

**Found in part F9 of the live check (2026-09-04), on the real body of
macports/macports-ports#34500.** Each member of a cohort is supposed to
carry its own link proof, or `links nothing that moved` where the sweep
found none. A member that failed to build carries neither:

```
— gegl-devel (graphics/gegl-devel): depends_lib; conflicts with gegl,
  which this cohort builds — bumped here, and needing a verification of
  its own
— gthumb (gnome/gthumb): depends_lib; nomaintainer
```

The withheld member explains itself where it is listed. The failed one
does not, and a reviewer reading "Revision bumped in this change" sees a
port with no evidence beside it and no reason for the absence. The fact
is in the body — "gthumb on Tahoe: the build failed, and this was
promoted anyway" — but it is in the verification block above, and
joining the two is left to the reader.

**Why it matters more now than it would have.** Since the dependents
became best effort, a failed member no longer blocks the promotion, so
this list will routinely carry ports that did not build. The line that
says why should be where the bump is claimed.

**Shape.** The member line already composes a reason from the candidate
and appends the link proof. Where a member has no proof because its run
did not reach one, it should say which: "the build failed, so nothing
was measured", "never built", "links nothing that moved" — the last
being a real answer and the others being absences with names.

## The body cannot be read without publishing it

**Anticipated by the live check and confirmed at F9 (2026-09-04).** No
verb renders the pull request body to a terminal. `--no-pr` pushes the
branch and prints nothing; the only way to see what a reviewer will read
is to open a pull request, which for this check meant opening one
against macports/macports-ports and closing it two minutes later.

Reading it here took a throwaway test in `internal/render` that opened
the repository, read the note off the branch tip and called `PRBody` —
the same call `promote` makes. That it worked is the point: the body is
a pure function of the record and could be printed by a verb.

**Shape.** `promote --body` (or `--dry-run`) writing the body to stdout
and doing nothing else. It needs the same inputs promote gathers — the
tip, the note, the fork owner, the duplicate-PR check — and the
duplicate check is a network call, so either the flag skips it and says
so in the output, or it runs and the flag is not quite free.

**Why it is worth a verb.** The one artifact a reviewer sees is the one
a maintainer cannot preview, and every change to how bodies are worded
is currently checked against goldens rather than against a real record.
The live check's own F9 says to record this as a gap rather than work
around it silently; the workaround above is the record of it.

## A stranger that stops the run is named as every later member's dependency

**Found in the confirmation cohort (2026-09-04).** `py310-rawpy` was
blocked because its dependency `py310-scikit-image` failed to build —
a stranger, outside the cohort, correctly named. The runner stopped
there. `py311-rawpy`, next in build order and never reached, settled
as:

```
py311-rawpy: blocked (Tahoe) — dependency py310-scikit-image fails to build; the change itself is untested
```

`py311-rawpy` depends on `py311-scikit-image`, not `py310-`. The
sentence asserts a dependency edge that does not exist.

**Why.** When the member the runner stopped inside is itself *blocked*
rather than failed, there is no failed sibling to name as culprit, so
every member behind it falls through to the stranger sentence — with
the stranger that stopped the run, not one of their own. The verdict
(blocked, untested) is right; the attribution is borrowed from a
sibling.

**Shape.** A member behind a stopper that was blocked by a stranger is
blocked by the *sibling*, not the stranger: "py310-rawpy could not be
built, so this member was not reached; it is untested." That is true
of it and names something a reader can check. Alternatively, keep the
stranger sentence but only where the stranger is in this member's own
`Requires`. Either way the sentence must not claim an edge the index
does not carry.

Low severity — the verdict and the gate are unaffected — but it is a
false statement in a body a reviewer reads, and it will become the
ordinary case once the runner no longer stops at the first failure.

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

## A bump that fetches nothing leaves its patches unchecked, and says nothing

**Found building D12 (2026-09-04).** Patch relocation runs only where
the plan fetched the target version's distfiles, because the new source
is the thing a hunk is relocated against. A bump that takes a branch
with no fetch — every distfile from a vendored block, say — proceeds
with its patches untouched, and the plan does not mention that they
were not checked. That is correct as far as it goes: no target, no
check. What is missing is the sentence. A plan that could not check a
port's patches should say so, the way the ABI finding says "unavailable"
rather than nothing.

## Nothing bounds the pull request body

**From the reassessment (2026-09-04).** No length check exists in
`internal/render` or `internal/gh`, and the push happens before
`gh pr create --body`. With the cap off, a cohort contributes one
verification line, one member line and its link-proof lines per member;
`libffi` has 132 dependents. Nobody has measured a body reaching
GitHub's limit, but "nothing bounds it" is a fact, and the failure mode
is the worst available: a branch pushed to the fork and no pull request
opened, with the author told nothing until `gh` refuses.

**Shape.** Measure the rendered body before the push. Over the limit,
refuse before anything leaves the machine, naming the size and the
limit — the same road as any other decline. Truncating the member list
with a count is the alternative, and it is the wrong one here: the
member lines are the evidence a reviewer is being asked to accept, and
a body that elides them is vouching for what it does not show.
