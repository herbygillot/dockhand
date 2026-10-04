# 2026-10-04: why the cancel test failed on Intel

CI's Intel job at 404ac160 failed `TestACancelIsAppliedByWhoeverHoldsTheRun` once: `RequestCancel` returned the run canceled where the test expected running. The Prime-time thread asked for the cause, since a cancel the holder misses would be a run stuck running.

## The cause

`RequestCancel` records the request, and applies it itself only where it can take the run's lease, which it can't while a live session holds the run. The holder reads the request at its next poll, here every 10 ms, ends the run canceled, and releases the lease. On a slow runner the holder did all of that between the request's record and its attempt at the lease; the request then took the free lease, and `finishCanceled` found the run already canceled and returned it as it was, writing nothing. The holder had applied the cancel, in time; the test asserted when, not who.

## What changed

The test now asserts what matters under either ordering: the request returns the run running or already canceled, the run ends canceled, and the event that ended it, `run.state` "canceled", was written by the holder's session, not the canceller's. It passed 200 runs under `-race`. Nothing in dockhand changed.
