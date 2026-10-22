# CX-6: a change record says what it was evaluated under

Roadmap part 2, item 1. `model.ChangeRecord.Runtime` is an
`EvaluatedUnder`: MacPorts Base's version and Tcl's, the platform the
interpreter described to the Portfile, and the host's where that was a
model, from the family snapshot's runtime (`macports.Runtime.Provenance`),
at both of the record's producers, the family evaluation (changes.go) and
an update's own evaluations (update.go). The record's JSON is kept whole,
so nothing migrates; one made before reads back with a zero runtime.

The plan doesn't read records yet. Where item 1's analysis makes it, a
record from another runtime is read by `UnseenChanges`' rule, as a
platform's is. Assessments keep their key until a failure is seen that
needs the runtime in it.

## L2's one-producer half

A change record was made two ways: the family evaluation's
(`revisionChanges`) and an update's own, from its before and after
evaluations, each setting the platform, the subports' changes, and the
unseen blocks itself. `changeRecord` (changes.go) is the one way now:
given the two sides' snapshots and Portfile texts, and a problem where a
side couldn't be evaluated, it sets the policy, the platform and runtime,
the subports' changes, and what this Mac doesn't evaluate, and says
nothing of subports where there's a problem. Both producers call it, so
what a complete record is has one definition.
