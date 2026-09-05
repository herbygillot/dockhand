# Cohort log corpus

Guest logs from a change with more than one member: one file per shape,
each with a sidecar stating the verdict every member must settle to.
It sits beside `testdata/logs`, which holds the single-subject corpus,
and it is swept the same way from the same two sides — the judgments
alone in `internal/verdict` (`JudgeCohort` over values, no repository)
and the effectful settle in `internal/engine` (a real repository, the
`verifytest` fake standing in for the guest). One copy of the files,
picked up by both with no code change.

A cohort log is `/tmp/dockhand-verify/log` in the worker after the
cohort runner has written to it (`internal/verify/tart/tart.go`,
`cohortRunner`). The runner announces each member with a marker line
before building it —

    ===> dockhand subject: oniguruma

— and it does not stop at a failure. Before each member it reads the
member's prerequisites (`requires.<i>`, the positions of the members
this one depends on, written from the request's own graph), and a
prerequisite whose state file says `failed` or `skipped` means this
member is skipped: it prints nothing, and its own state file says
`skipped` and names the prerequisite. Every other member is built
whatever happened around it.

One member may take a step ahead of its own build: a member the caller
forced into the cohort (the D24 override) carries a `before.<i>` file,
run first through the same line as its lint and install and after its
prerequisite check, that deactivates the sibling it conflicts with. Its
output lands in that member's own section, because the marker was
already printed; a deactivate that fails is that member's failure, taken
down the same `failed` path as any other step, and the loop goes on —
D25's runner, with one more step for the judge to trust the state file
about.

The guest therefore leaves two records, and the judge reads both:

* the log, cut on the markers — a member's own section is what its
  verdict is read from: its lint line on a pass, its diagnosis on a
  failure, the stranger it blames when a dependency broke under it;
* the runner's record, one word per member (`state.<i>`, read back
  through `verify.MemberStater` and stated here as `<port>.reported`)
  — `passed`, `failed`, or `skipped <member>`. It is what tells a
  member skipped on purpose from one the runner never reached, since
  neither prints a marker; the judge trusts it (D25).

What follows from that, and what these files exercise:

* a member whose record says it passed is passed on its own section,
  whether the members around it failed or not;
* a member whose record says it failed is judged on its own section
  exactly as one subject is — failed, unsupported, or blocked on a
  stranger — and a section naming a SIBLING blocks the member on the
  sibling, in the sibling's words;
* a member whose record says it was skipped is blocked, blamed on the
  prerequisite the record names, in a sentence true of that
  prerequisite: "fails to build" where it failed, "could not be
  built" where it was itself skipped or blocked;
* a member the guest neither announced nor recorded, in a cohort that
  announced or recorded others, is errored rather than blocked: the
  runner finishes every member it is given, so that silence is a
  runner that did not, and a pass invented for it would publish a
  port nobody built.

A log with no record beside it — a provider that cannot read one, or
a guest whose runner wrote none — is read on its markers alone: the
last member the FILE announces is the one the guest was inside when it
gave up, and a member announced before it with a clean section
finished. The marker bytes are `verify.SubjectMarker`'s,
`verify.SplitSubjects` cuts on them, and a log with no marker at all is
one implicit subject returned whole — which is why the single-subject
corpus next door needs none of this and settles exactly as it always
has.

## Dropping in a real capture

    dockhand log <branch> > internal/engine/testdata/cohorts/<name>.log

Then write `<name>.expect` by hand from what you saw in the field —
the `reported` words are what `state.<i>` in the guest said — and run
`go test ./internal/engine/ ./internal/verdict/`. Name the file for
the shape rather than for the ports, since a cohort has several. Every
file here is SYNTHESIZED — written from the runner's own script and
from MacPorts output shapes, not captured from a guest — and every one
of them should be replaced by a capture when one lands: keep the name,
rewrite the sidecar's provenance comment.

## The sidecar

`key: value` lines. `#` comments and blank lines are ignored; a value
runs from the first colon to the end of its line, so a detail may
itself contain colons. `members` must come before any member's own
key, and keys other than these fail the sweep.

| Key | Meaning |
|---|---|
| `members` | The change's ports, space separated, in build order; `members[0]` is the headline. Required, and it is the roster the blame reader matches against. |
| `outcome` | What the guest's aggregate state file said: `passed` or `failed`. Required. |
| `<port>.state` | The state that member's note settles to: `passed`, `failed`, `blocked`, `unsupported` or `errored`. Required for every member. |
| `<port>.detail` | The detail that member's note carries. |
| `<port>.blamed` | The SIBLING whose failure this member inherited. Set only on a blocked member, and only ever another member — a port outside the change rides the detail instead. |
| `<port>.lint` | What that member's own section said: `clean`, `1 warning`, `N warnings`, or empty. The note records it only on a pass, and stating it per member is what proves the log was cut. |
| `<port>.reported` | What the runner's own record said about that member: `passed`, `failed`, or `skipped <member>` naming the prerequisite it was skipped for. Absent for a member the runner wrote no state file for, which is the shape of a runner fault — or of a whole guest that wrote no record, which is the log read alone. |
| `<port>.forced` | The sibling this member's build deactivated first — the D24 override, recorded on the run at submit and carried through settle. Set only on a forced member, and it names another member of the cohort (the seated sibling), never the headline. |

No `nomaintainer` annotation appears here, for the same reason it does
not next door: both sweeps settle against a tree holding no
dependency's Portfile, so the lookup always answers no.

## Provenance

| File | Shape |
|---|---|
| `pass-two-subjects` | Both members build cleanly. Their lint lines differ, so an uncut read would give both the first summary in the file. |
| `independent-behind-failure` | The first member breaks and the member behind it does not depend on it: built after the failure, passed on its own section. The live shape this corpus was extended for (mise behind oniguruma6). |
| `dependent-behind-failure` | The headline breaks and the member behind it depends on it: skipped, never announced, blocked on the headline by the record's word. Its log is silent about the second member exactly as `passed-unannounced`'s is; the record is what tells the two apart. |
| `chain-behind-failure` | A chain, A ← B ← C, and A breaks. B is skipped for A and C for B; each is blamed on its own prerequisite, and the sentence changes down the chain — B is told A fails to build, C that B could not be built. |
| `middle-failure` | The headline passes, the second member breaks, and behind it one member that needs only the headline passes while one that needs the broken member is skipped. Four members, four answers. |
| `stranger-failure` | One member passes, the next is built, and a port outside the change breaks under it. Blocked on the stranger, blaming no subject; the record's `failed` does not override the log's reading of what failed. |
| `sibling-dependency` | The headline's install pulls a sibling out of the overlay and the sibling breaks, under the headline's marker; then the runner builds the sibling in its own turn, and it breaks again in its own section. The roster is what tells a sibling from a stranger. |
| `interleaved-test-phase` | Both members announced twice, build then test, with a failure in the second visit — not what the runner writes, but the shape `verify.SplitSubjects` permits, and the log read ALONE (no `reported` keys): sections must accumulate, and the last marker in the file is where the guest gave up. |
| `passed-unannounced` | A passing job that announced only one of two members. The unannounced one is errored, never passed: a promotion sums the passes, and one invented here would publish a port nobody built. |
| `forced-member` | A forced member (the D24 override): the headline and the seated sibling build in the cohort's environment, then gegl-devel builds last in one gegl was deactivated in first. Its section opens with the deactivate — its own words, under its own marker — and it passes; its run carries the sibling it was built without. The deactivate names gegl inside a section that passed, so the blame reader never runs. |
| `forced-deactivate-fails` | The one step ahead of a forced member's build failing: gegl's own build broke, so it was never installed and `port -f deactivate gegl` throws before gegl-devel is linted. The forced member does not require gegl, so the runner reaches it (D25: the loop goes on past a failure and skips only what depended on it) and it fails on its own section in MacPorts' own words, blaming nobody — "port deactivate failed: …" is not the "Failed to <phase> gegl:" shape a dependency failure wears — and its run still carries the sibling it was told to deactivate. |

Two files left with the runner that stopped. `member-failure` was the
old runner's dependent-behind-the-headline shape, its second member
blocked on the strength of silence alone; its log is now
`dependent-behind-failure.log`, unchanged, with the record beside it
that the runner writes today — the same silence in the log, and the
word that makes it a skip rather than a fault.
`interleaved-return-failure` pinned the STOPPER — the one member the
runner was inside when it gave up — as read from the file's order
against the roster's, and blocked a member on that stopper by its
position in the roster. There is no stopper now: a member is judged on
its own record and its own section, and its position in the roster
decides nothing. The one point of it that survives, that a log read
alone takes the last marker in the file's order, is held by
`TestTheLogAloneTakesTheLastMarkerInFileOrder` in `internal/verdict`.
