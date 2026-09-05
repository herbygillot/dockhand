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

## A changed member whose version and revision did not move is installed from the archive, not built

**Found in the runner's live proof (2026-09-05), held for ruling.** Since
the overlay carries `_resources`, MacPorts in the guest reaches
packages.macports.org, and `port install` of a port whose name, version
and revision match an archive there installs the archive and runs no
build phase. A three-member branch with `build.cmd false` added to
`devel/oniguruma6` at its standing `6.9.10_0` came back `passed`,
`passed`, `passed` inside a minute; the same branch with the revision
moved to `_1` built from source and failed as sabotaged, its build
dependencies and `tree` still coming from archives. The 2026-09-03 live
check saw the failure only because the overlay then lacked `_resources`
and the archive fetch had no sites — a defect since fixed was doing the
from-source work by accident.

**Where it bites.** Only the hand-made branch road. A version bump's
headline and a revision bump's members move to a `version_revision` no
buildbot has built, so no archive exists for them; and `bump --recheck`
and `refresh-checksums` are already marked from source
(`engine.Policy.fromSource`), for exactly this reason. What has no
from-source decision at all is `verify <branch>`:
`submission.fromSourcePorts` names only the submission's own headline,
and a branch has none. A maintainer who fixes a configure argument
without a revbump — a change MacPorts policy says needs one — gets a
green verification that built nothing of theirs.

**The seams already exist.** `verify.Request.FromSource` is per port,
and `build.InstallArgs` puts `-s` on that member's own install line, so
that port builds from source while the archives for everything else
still serve. What is missing is the decision on the branch road: at
submit, a member whose version and revision equal the merge-base's —
one `git show` of the merge-base Portfile, and `eval` already reads a
Portfile's version and revision — is one the archive would satisfy.

**Two shapes, for ruling.** (1) Mark such a member from source and say
so on stderr — "oniguruma6 changed at 6.9.10_0; building it from source
so the change is what is tested". The verification then tests the
change, at the cost of building that member from source. (2) Say it and
do nothing — a finding in the `patches-unchecked` shape, "oniguruma6
changed but neither version nor revision moved; where a binary archive
exists it is installed and the change goes unbuilt" — and let the
maintainer move the revision, which policy asks of them anyway. (1) is
the honest verification; (2) is the cheaper one and points at the real
omission. Either is small; neither is made.

## On the branch road, members are built in portdir order, not dependency order

**Left by the review of the runner change (shipped 2026-09-05 as
`8579bfd`).** `SubjectsOf` orders a branch's members by portdir, and
`cohortRequires` spells the graph over that order, so a member's
`requires.<i>` may name a *later* position. The runner then builds the
dependent before its prerequisite: no state file exists yet, so it is
not skipped, and it fails on its own with the prerequisite's failure in
its log — the reading the judge has always made, at the price of a
build that a topological order would have skipped. Never a wrong
verdict; a slow one. The revbump road is unaffected: the headline is
first and members follow by name, and members of one headline rarely
depend on each other. A topological sort at submit, stable within ties,
would close it. In the same place: `launch` writes a `requires.<i>` for
every member, empty ones included, one `tart exec` each; the runner
tolerates a missing file, so the empty ones need not be written.

## What remains of `status`/`cycle`

**What remains of "`status` should only report; a `cycle` verb should do
the work" and of "`verify` promises a view that cannot show the branch"
(both shipped 2026-09-05 as `8daff2f`).** The split landed whole, with
twelve points the design left open ruled by the implementation and
stated as pending where each lands in the code (the run log has the
list). Five facets the review left behind, none a defect:

1. **`cycle`'s own report names `cycle`.** The report is taken before
   the drain by design, so a queued run this pass starts reads
   "`dockhand cycle` starts it" on stdout above the "verify: submitted"
   line on stderr. A reader of stdout alone is told to run the verb they
   just ran. `Report.Text` could drop the remedy when it carries drain
   lines.
2. **Four spellings of one remedy**, each a one-for-one carry of the old
   `status` spelling: "starts it" (render), "starts it when it can"
   (`verdict.QueuedError`, `engine.VerifyDeferredError`), "starts it when
   one frees" (`cli.md`, `exitcode`), "retries them as remedies are met"
   (the verify summary). Consistent relative to before; one spelling
   would be better.
3. **`internal/verify/verify.go`'s doc comments still name `cycle`.**
   The ruling that a provider package names no CLI verb is met by the
   recorded sentence (`CapacityError.Error()`); the prose reintroduces
   the coupling in comments only.
4. **The reclaim-before-drain order is pinned by code and comment only.**
   `verifytest.Fake` keeps Released and Submitted in separate slices
   with no shared timeline, so no test can assert that the slot reclaim
   freed was the one the drain used. A timeline on the fake would pin
   it.
5. **A noted hand-made branch is shown, settled and drained, and nothing
   else.** `inFlight` (revbump-proposal exclusion), the sweep's standing
   branches, `supersedeSiblings` and `Resolve` stay on the namespace, as
   the readers recommended: each is a fact about what dockhand minted.
   Whether a hand-made branch verifying a port should keep a proposal
   from naming that port again is a question, not an omission.
