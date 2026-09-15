# Canceling tracking of shared GitHub runs

GitHub verification adopts push-triggered runs that can predate the request or serve other jobs. Those observations do not establish exclusive cancellation authority. Cancel now releases only this request's tracking under its request lock, using the existing provider execution `released` state. It requires no remote API access, survives restart, fences stale submissions, and produces an explicit local canceled observation without a fabricated remote workflow conclusion. Trace stops waiting for logs after tracking is released.

Removed the unused Actions cancellation adapter and its test now that Dockhand does not issue remote cancellation calls. Tart continues to stop its own VMs through its existing lifecycle. No database migration is needed.

Regressions cover two requests adopting the same run, invalid handles, offline and repeated cancellation, restart, stale submission, reconciliation after a lost response, unaffected passing evidence for the other observer, and driver completion after admitted-job cancellation.

Validation: GitHub provider and forge adapter suites passed. The uncertain-submission regression now waits for persisted driver state rather than assuming its first retry is already due.
