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
