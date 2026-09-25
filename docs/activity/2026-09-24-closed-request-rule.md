# 2026-09-24: one rule for a closed request's submit

Step 1 of the roadmap's Next, carried from the review follow-up: the Tart
and GitHub providers decided the closed-request refusal each their own
way. The contract (`verify.Provider.Reconcile`) is that a closed request
refuses every later Submit, including a stale driver's.

They disagreed twice. Which rows were closed: Tart counted a closed row
and a row released without a result; GitHub every released row, although
its own Reconcile reports a released row that had admitted a run as a
found run. And the answer: Tart returned an error; GitHub an Unsupported
submission, which the workflow records as a verdict, "provider rejected
the build as unsupported", for a build that never ran.

`verify/ledger` now holds the rule both use:

- `ledger.SubmissionClosed`: closed, or released with nothing recorded.
  A row released with a result keeps its run (the run Tart finished, the
  run GitHub admitted, whose identity GitHub keeps in the result), so a
  repeated Submit gets that admission back, as Reconcile finds it, and
  starts nothing; observing it reports what happened, cancellation
  included.
- `ledger.ErrClosed` is the refusal, from both providers. The attempt
  goes uncertain and reconciliation, which reports the request closed,
  settles it.
- A rejection GitHub recorded durably at the first Submit is still
  answered again as that Unsupported submission: it is the same answer
  to the same request, not a refusal.

Changed expectations: a stale GitHub submit after a cancellation that
followed admission returns the admitted run rather than Unsupported; a
submit of a request reconciliation closed returns `ledger.ErrClosed` from
both providers. `ledger.TestSubmissionClosedIsOneRule` pins the rule.
