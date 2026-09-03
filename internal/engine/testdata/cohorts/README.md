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

— and stops at the first member that fails. Those two facts are the
whole of the attribution, and they are what these files exercise:

* the member the FILE announces last is where the guest gave up, and
  the file's order is what is read — a runner that returns to a port
  it already built announces it again, and the roster's order would
  then name a member the guest had already left;
* a member announced before the stopper, whose own section carries no
  failure, ran every command it was given and every one of them exited
  zero;
* a member never announced was never built, and its silence is the
  evidence for that rather than an absence of evidence — including a
  member the runner skipped and then stopped past, which is nobody's
  fault to inherit and is recorded as errored rather than blocked.

The marker bytes are `verify.SubjectMarker`'s, `verify.SplitSubjects`
cuts on them, and a log with no marker at all is one implicit subject
returned whole — which is why the single-subject corpus next door needs
none of this and settles exactly as it always has.

## Dropping in a real capture

    dockhand log <branch> > internal/engine/testdata/cohorts/<name>.log

Then write `<name>.expect` by hand from what you saw in the field, and
run `go test ./internal/engine/ ./internal/verdict/`. Name the file for
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

No `nomaintainer` annotation appears here, for the same reason it does
not next door: both sweeps settle against a tree holding no
dependency's Portfile, so the lookup always answers no.

## Provenance

| File | Shape |
|---|---|
| `pass-two-subjects` | Both members build cleanly. Their lint lines differ, so an uncut read would give both the first summary in the file. |
| `member-failure` | The headline breaks and the second member is never announced: failed, and blocked on it. |
| `stranger-failure` | One member passes, the next is reached, and a port outside the change breaks under it. Blocked on the stranger, blaming no subject. |
| `interleaved-test-phase` | Both members announced twice, build then test, with a failure in the second visit. Sections must accumulate. |
| `interleaved-return-failure` | The same shape with the failure on the FIRST member's return visit, where the file's order and the roster's disagree about who the stopper is. Reading the roster's would pass the port that broke and condemn the one that did not. |
| `sibling-dependency` | The headline's install pulls a sibling out of the overlay and the sibling breaks, under the headline's marker and without a marker of its own. The roster is what tells a sibling from a stranger. |
| `passed-unannounced` | A passing job that announced only one of two members. The unannounced one is errored, never passed: a promotion sums the passes, and one invented here would publish a port nobody built. |
