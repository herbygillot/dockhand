# Multi-target verification scheduling

Generalized verification planning and execution from one attempt per job to one isolated attempt per planned target. This establishes the workflow half of dependent verification while keeping reverse-dependent discovery and coverage policy separate.

The verifier now creates an immutable target roster and one build specification for every accepted job target. The workflow stores the plan, attempts, and initial submission identities together before any provider call. Its execution snapshot loads every attempt, current submission, and owned resource for the job. Each cycle claims and performs one provider action, while concurrent drivers can claim other ready attempts through the existing SQLite transaction boundary.

A terminal attempt no longer settles the job by itself. The workflow waits until every attempt is terminal, continues independent work after a failed or unsupported target, and then derives the job outcome from the complete verdict set. Failed evidence takes precedence over needs-attention and cancellation outcomes. A successful roster advances combined publication only after all targets pass. Cancellation is applied to every queued or running attempt, and each attempt retains its own cleanup disposition and diagnostic evidence.

The existing one-target evidence reuse policy remains unchanged. Multi-target jobs execute fresh attempts because reuse needs a per-target coverage decision rather than the current job-level `ReusedAttempt` field. CLI selectors still bind one initial target, and reverse-dependent discovery does not yet add coverage targets; those remain the next part of the roadmap item.

Removed the unused placeholder `verify.Planner` and generalized the existing concrete planner instead. All implementation and tests in this change were authored for v2; no v1 comments, tests, or source were copied.

Validation covers mixed pass/fail completion, continued work after partial failure, whole-job cancellation, existing capacity and reconciliation behavior, the full Go test suite, race detection, vet, production build, and whitespace checks.
