# Canceling uncertain GitHub submissions

Added cancellation intent to the verification provider reconciliation contract. The driver freezes that intent with its attempt claim; Tart keeps its existing reconciliation behavior, which already stops unadmitted provisioning.

GitHub now closes a reserved request under its request lock without looking up runs or pushing. Closure survives restart and rejects stale submissions. An already-sent push may still trigger Actions, and the recorded cancellation detail explicitly says so.

Regression coverage exercises cancellation through the driver before and after the initial push, with GitHub unavailable, then retries submission through a reconstructed provider and checks that the remote branch did not change.

Validation: GitHub, Tart, and workflow package tests passed. The first test invocation exposed a missing test import after the interface change; that import was corrected and the provider suites rerun successfully.
