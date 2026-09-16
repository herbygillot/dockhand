# Review: bump through publication

Findings from tracing `dockhand bump <port> [version] --publish` from CLI intake to a
confirmed pull request, against the tree at `fee2209`. These are observations awaiting
triage, not accepted priorities and not completed work; see the [roadmap](../roadmap.md)
for the current queue and `activity/` for implementation history.

The traced path holds together. Intent is immutable after acceptance, every external call
sits outside its state transaction with a claim revalidated on return, and each phase
transition is a durable checkpoint. The findings below are defects and inconsistencies
within that structure, not objections to it.

## 1. A rate-limited pull-request write settles the job permanently

`publicationError` in `forge/github/publication.go:77` maps `403 Forbidden` to
`forge.ErrRejected` alongside 400, 401, 404, 409, and 422. GitHub returns 403, and
sometimes 429, for both primary and secondary rate limits. `go-github` v91 distinguishes
these as `*github.RateLimitError` and `*github.AbuseRateLimitError`; nothing under
`internal/` references either type.

A rejection is definitive by contract. `runPublication` treats `forge.ErrRejected` from
`Service.Write` as terminal and calls `finishPublication` with `JobNeedsAttention`, which
`record.JobState.Terminal` reports as terminal. The publication action has already set
`WriteStarted`, so the convergence loop will not reissue the request either. A transient
rate limit therefore leaves a pushed branch, no pull request, and a dead job that only a
fresh publication request can recover.

This contradicts the stated contract in `cli-design.md`: a missing or rejected credential
settles while the operation is known not to have started, and transient API failures remain
retryable. A rate limit is the second case being classified as the first.

Direction: test the typed rate-limit errors before the status switch and let them reach the
retryable path. `forge/github/publication_test.go:115` asserts the present 403 behavior and
needs splitting into a permission rejection and a rate-limited retry.

## 2. Retry scheduling has no backoff and does not distinguish waiting from failing

Every retry site schedules `now.Add(c.retry)`, which `newCycle` defaults to one second:
`cleanup.go:114` and `:147`, `preparation_integrate.go:77`, `publication_plan.go:53`,
`publication_run.go:264`, and `verification_run.go:164`. `proc.Manager` polls on the same
one-second default. There is no escalation, jitter, or ceiling anywhere.

Three consequences are worth separating.

Capacity waiting is charged at failure cadence. An `AtCapacity` submission returns the
attempt to `AttemptQueued` with no detail, so every queued job re-runs `tart list` and
reacquires its submission lock once per second until a slot frees. Waiting for expected
capacity is not an error and should poll slowly.

Publication confirmation polls remote services once per second. Each pass through
`runPublication` calls `Service.Observe` against the forge API and `RemoteHead` against the
remote. Combined with the absence of any rate-limit handling in the forge client, this is a
plausible cause of finding 1 rather than an independent concern.

Permanent misconfiguration never escalates. A `providerReady` mismatch records
`LastError` and retries in one second, indefinitely, with no terminal state.

Direction: separate expected waiting from failure, and give the failure path capped
exponential backoff with jitter keyed on consecutive failures.

## 3. Preview and execution use differently authenticated GitHub clients

`app.Build` attaches keychain credentials to its client when no custom base URL is
configured (`app/app.go:76-78`). `PreviewPreparation` constructs its own client and does not
(`app/preparation.go:50`).

Release discovery under `bump <port> --diff` therefore runs against the unauthenticated
rate limit while the same discovery under `bump <port>` runs authenticated. A preview can
fail where the real invocation succeeds, which inverts the purpose of a preview. The
existing note in `cli-design.md` that public reads and `publish --dry-run` may succeed
without authenticating concerns publication preconditions, not release discovery, and does
not cover this divergence.

Direction: one shared client constructor used by both paths.

## 4. The two verification providers fail at different points for the same condition

`buildResolver` in `app/preparation.go:117` returns a hard error on the GitHub branch at
`:123`, bypassing the `preserve` behavior that the Tart branch relies on at `:134`. The
same condition, no usable build configuration, aborts before any branch exists under
`--provider github` but produces a prepared branch and a needs-attention job under Tart.

Provider selection should not change where in the lifecycle a failure surfaces. Decide
which behavior is correct and apply it to both.

## 5. The single-target constraint is expressed three times in three ways

`verification_bind.go:195` rejects multi-target selections explicitly and with a clear
message. `preparation_run.go` assumes `Targets[0]` implicitly. `publication_plan.go:88`
requires exactly one attempt and returns a bare `ErrInvalidRequest` with no detail.

This is not currently reachable as a defect, because binding rejects multi-target
selections before preparation or publication can observe one. It is a latent coupling: the
roadmap plans multi-target intake and per-target dependent verification, and the
publication expression would surface as an unexplained invalid request after a successful
build. State the constraint once, with one message, in the place that owns it.

## 6. Unknown provider names fall back silently

`Engine.VerificationProvider` at `engine.go:102` returns the single-provider fallback for
any name absent from `Providers`. A job recorded against a provider the running binary does
not have routes to the fallback instead.

`providerReady` catches the mismatch by comparing the configured provider against observed
capability name, so this is not a data-integrity hole. The cost is diagnostic: the user
sees a confusing mismatch between two provider names rather than the real problem, and by
finding 2 the job retries that message every second forever. Returning nil for an unknown
name would let the error name the actual condition.

## 7. `--publish` without `--wait` does not publish

`Reached` treats the admission milestone as satisfied once the job is admitted, has reused
evidence, or has reached an active publication phase. A `bump --publish` invocation
therefore exits at VM admission, printing that work remains pending, having performed no
publication.

This is deliberate and documented: reuse satisfies admission and leaves publication
pending, and only confirmation completes the job. It is recorded here as a usability
question rather than a defect. For a flag whose only purpose is producing a pull request,
defaulting to the completion milestone, or warning explicitly that nothing will be
published without a running driver, may match intent more closely.

## Not findings

The following were examined and are working as designed; they should not be re-raised from
this review.

Provider routing is already generalized. `Engine.Providers`, per-name capability
observation in `cycle.checkProvider`, and provider-scoped cleanup and pruning in
`cleanup.go` and `retention.go` handle the second provider correctly.

Preparation commit determinism is intentional. `prepareCandidate` signs with
`job.AcceptedAt` rather than wall-clock time, so a retried preparation reproduces the same
commit identity.

Refusing to recreate an absent branch during interrupted integration is correct.
`integrateBranch` cannot distinguish a never-created branch from a user-deleted one, and
requiring attention is the safe resolution.

Setting `WriteStarted` before issuing the pull-request call is correct. Absence of a
response cannot prove the request never applied, so observation-only recovery is the right
posture. Finding 1 is about misclassifying which failures are definitive, not about this
checkpoint.
