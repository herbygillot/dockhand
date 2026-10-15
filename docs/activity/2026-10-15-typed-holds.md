# Typed holds (C2a), with a hold said once (v0.4.0 part 1, b)

Holds were strings in five places: `ServeCandidate.Held`, `Engine.Held`,
`HeldBeforeEdit.Held`, `BranchStatus.Held`, and `assessedHolds`. They're
`[]model.Concern` now, so the majors hold, the new holds, and `ship --yes` are
written once against the type layer 1 keeps.

- `model.UpstreamComparison.Concerns(port)`: a comparison's holds, typed,
  which `Holds()` and `SubmitPlan.Concerns` both read rather than each
  building them. `model.FromSubmission` is the origin of a blocking reason
  or a plan that couldn't be made.
- serve says a held branch once while its first concern's `Key()` stays the
  same, where it compared words.
- `engine.HoldLines` says concerns a line each, the same finding of several
  ports once (`Concern.Unported`): py-kubernetes's two subports' held line
  read "…follow; upstream's setup.py changed; …follow". That's the second
  half of the roadmap's py-kubernetes entry.
- JSON: a branch's `held` keeps its words, and `concerns` beside it carries
  each with its origin, port, and rule; submit's `concerns` reads the same
  view (`concernsView`).

Tests: TestAHoldSharedByPortsIsSaidOnce; the holds tests read `HoldLines`.
