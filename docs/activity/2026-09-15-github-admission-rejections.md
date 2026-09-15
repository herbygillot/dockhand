# Durable GitHub admission rejections

GitHub preflight now treats known contribution-base conflicts and explicit authentication/missing-workflow responses as rejections before any push intent is recorded. Rate-limit/temporary responses and transport errors remain retryable. Rejections retain their reason in closed provider state; reconciliation carries that disposition back to workflow after a lost reply instead of creating another submission identity. Cancellation still takes precedence.

Driver regressions cover merged, missing, and unrelated bases, authentication and workflow failures, lost replies, restart, and temporary 403/429/503 recovery. No external services are exercised.

Validation: GitHub and workflow suites passed, including the deleted-local-branch case.
