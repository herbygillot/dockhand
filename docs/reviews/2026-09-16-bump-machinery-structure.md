# Review: structure of the bump-to-PR machinery

An assessment of the packages active from `dockhand bump <port>` to a confirmed pull
request, against the tree at `f260f03`. The question was whether new concepts, package
splits, or utility packages would simplify the machinery. These are observations awaiting
triage, not accepted priorities and not completed work; see the [roadmap](../roadmap.md)
for the current queue and `activity/` for implementation history.

The architecture holds and should not be restructured. One cycle claims eligible work, an
`execution` unit of work carries the records for one transition, writes are diffed against
what was loaded, and every external call sits outside its transaction with the claim
revalidated on return. The findings are about the code that implements that design by hand
in several places, and about small mechanics repeated across packages with drifting
details. Sizes for orientation: `workflow` is 5.7k lines of code, `portedit` 2.9k,
`verify/tart` 1.7k, `state/sqlite` 2.2k.

## Concepts

### 1. A typed outcome for a phase step

Every runner returns `(changed bool, detail string, err error)`. Whether a step is waiting
for something expected or has failed is carried by two conventions, neither of which is a
type: an empty `detail` means "not a failure" in `verification_run.go:149-165`, and a
variadic `problems ...error` means "is a failure" in `publication_run.go:273-285`.

The two conventions already disagree. `verify.SubmissionUncertain` and `RunUnknown` are
classified as waiting when the provider filled in `Detail` and as failure when it did not
(`verification_record.go:61-67`, `:151-156`), so a provider that always fills `Detail` never
backs off and never accumulates failures. `publication_run.go:76` routes a missing
publication service, a configuration fault, through `publicationRetry` with an error, so a
permanent misconfiguration gets exponential backoff instead of needs-attention. Waits are
never counted: `waitingDeadline` zeroes `ConsecutiveFailures`, so "Waiting for the forge to
observe the pushed branch" can loop at the wait interval indefinitely.

Direction: an `outcome` value with three constructors, waiting with a reason, failed with
an error, and terminal with a state and detail, returned by `recordAttempt`, the
publication recorder, and the post-call transactions. This is the highest-value change in
the list because it forces each misclassification above into the open.

### 2. A claimable lease embedded in Job, Attempt, and Resource

The tuple `ClaimGeneration`, `Claim`, `RetryAt`, `ConsecutiveFailures`, `LastError` is
repeated verbatim on `record.Job`, `record.Attempt`, and `record.Resource`, and is
manipulated through loose pointers (`c.claim(&gen, ...)`, `failureDeadline(key, &failures,
err)`). Three symptoms:

- Six terminal-marking copies. `finishPreparation` (`preparation_integrate.go:14`) clears
  `Claim` and `RetryAt`; the copies at `verification_plan.go:58,66,81`,
  `verification_record.go:207`, and `publication_run.go:315` set only `State`,
  `FinishedAt`, and `Detail`. The store nulls a terminal job's `next_action_at`
  (`state/sqlite/records.go:425`), so the stale `RetryAt` is a record inconsistency rather
  than a re-selection loop, but it is the kind of drift a shared helper prevents.
  `finishPreparation` is the generic helper misnamed after one phase; `dependents.go:53`
  already uses it in the verification phase.
- Five revalidation predicates. `preparation_run.go:92` checks terminal, prepared, and
  ownership; `preparation_integrate.go:77` adds phase and result revision;
  `dependents.go:105` and `publication_run.go:126` check only terminal and ownership, so a
  plan or publication update can land on a job whose phase moved; `verification_run.go:143`
  adds an attempt-state check nobody else has.
- Three hand-rolled deadlines bypass `failureDeadline`/`waitingDeadline`:
  `verification_run.go:159,163` and `cleanup.go:148`, the last a verbatim copy of
  `waitingDeadline` including the failure reset.

Direction: one embedded type in `record` with claim, release, fail, and wait methods, one
`finishJob` helper, and one `claimGuard(job, phase, expected, now)` predicate. Cancellation
is checked at a different point in each runner (`preparation_run.go:96` first,
`preparation_integrate.go:128` last and deliberately so, `dependents.go:110` first while
discarding a computed plan); the guard should make that choice explicit rather than
incidental.

### 3. Smaller named things in `workflow`

- `publicationInput` (`publication_policy.go:83`) returns an unnamed revision-and-source
  pair from seven call sites; `Job.EffectiveSource()` names it.
- `BoundPreparation` (`preparation_bind.go:38`) and `BoundVerification`
  (`verification_bind.go:45`) are the identical `{Request, Snapshot}`.
- `FindRepository(...).ID != e.Repository` appears at `preparation_bind.go:53`,
  `preparation_run.go:146`, `preparation_integrate.go:149`, and `publication_bind.go:52`.
- The publication precondition (open change, current revision, branch equals source
  branch) is spelled out at `publication_plan.go:79,156` and `publication_run.go:156,184`.
- `attemptAction` (`verification_run.go:14`) is the counterexample done right: because it
  is typed, its three switches read well. `runPublication` is the same machine with the kind
  derived inline from `(WriteStarted, remote == desired, observed.Found)` in an if-chain at
  `publication_run.go:203-270`.

### 4. A workspace in `portedit`

`workspace` (`portedit/source.go:81`) is a one-field struct with no behavior while
`files.Root` appears 33 times, joined into a portdir at seven sites and into a Portfile
path at six. The edit-evaluate-restore cycle exists twice, `evaluateContents`
(`source.go:93-124`) and `observeContents` (`observation.go:43-51`), with eleven and six
callers, and the `(ctx, request, input, contents, bool)` signature repeats at each.
`evaluateContents` returns `input.files.Root` as its root unconditionally (`source.go:123`),
so the `beforeRoot`/`afterRoot` pairs threaded through six fidelity comparators never
differ inside the package; only `preparation.go:165` passes different roots.

Direction: give the workspace `withContents`, portdir and Portfile helpers, and ownership
of the baseline observation memo currently bolted onto `sourceInput`; replace the
`(portfile.Edit, Snapshot, string, error)` quadruple with a result struct and drop the
constant root. A `versionInputs` type over the carrier set would isolate the ambiguity
rules at `version_edit.go:136-192`, and an `archiveStore` would replace the service-clone
trick that is the only way archives reach disk (`dependencies.go:182-183`).

### 5. `JobSpec` and `Request` are unions

Twelve of about twenty `JobSpec` fields are conditional on the action (`record/job.go:52-93`),
and `normalizeSpec` (`request.go:23-226`) is the hand-written discriminator.
`portedit.Request` is likewise a union enforced by `Validate`, and it is re-exported as
`preparation.Request` (`preparation.go:17-24`) even though `Root` is a portedit-only
concern that real callers never set. This is defensible for a CLI-driven service. The
fixable parts are `Root` and the alias, not the union.

## Package splits

- **Real today: a `workflow` policy leaf.** `verification_select.go`,
  `verification_reuse.go`, `publication_coverage.go`, and `publication_policy.go` are
  read-only policy over `state.Reader` with no dependency on `cycle`.
- **Conditional: intake versus drive.** Engine field usage supports it: `Ports` is
  intake-only; `Preparer`, `Releases`, `Dependents`, and `Provider` are drive-only. But
  `publicationEvidence` (`publication_policy.go:15`) is called from both `submit.go:167` and
  `publication_run.go:159` and pulls in `selectVerification`, `publicationCoverage`, and
  plan reads. If that trio cannot move into the policy leaf cleanly, do not split; the
  result would be a mutual dependency dressed up as two packages.
- **Real in `portedit`: fidelity.** `fidelity.go` plus the release-scope comparators are
  pure snapshot comparison with no `Service` dependency, and `CheckEquivalent` is already
  consumed from `workflow/preparation`. `download.go` and its archive-source helpers are
  also extractable with no cycle, at smaller gain.
- **Seams in other packages.** `portindex/index.go` mixes tool resolution, environment
  policy, the build, and file utilities; the cache policy already lives in `cache.go`.
  `verify/github/provider.go` would read as precondition, effect, record with its state
  plumbing in a small store type. `verify/tart/native.go`'s guest scripts and `logs.go:49`
  belong beside `tart/host`; the type assertion at `logs.go:40-45` marks the seam being in
  the wrong place.
- **Not worth it.** A separate publication package: `planPublication` and
  `runPublication` touch Job, Change, Revision, Attempt, and the claim and retry machinery,
  so the boundary would be circular.

## Utility packages

The same five lines are written many times across `git`, `verify`, `tart`, and
`portindex`, with details that drift.

- **Extend `filelock`.** Add a digest-named path helper, a with-lock callback, and a
  try-or-skip form. That removes the second flock implementation in `git/branch.go:48-86`
  (git keeps only the wrapper that carries the descriptor in the context for child
  inheritance), four different digest helpers (`verify/github/config.go:54`,
  `verify/tart/execution.go:117`, `portindex/index.go:30`, `tart/digest.go:8`), and two
  identical skip-if-busy blocks (`github/retention.go:35-38`, `portindex/retention.go:48-53`).
  Two inconsistencies were introduced during the PortIndex consolidation: the generation
  lock at `portindex/cache.go:143` falls through to a blocking acquire on any non-busy error,
  including a context error.
- **Extend `atomicfile`.** Add a create-via-writer form and a directory-replace form. Of
  six temp-then-rename sites (`atomicfile/write.go:11`, `portindex/cache.go:275`,
  `portindex/index.go:280-376`, `verify/staging/archive.go:67-150`,
  `verify/github/logs.go:141`, `verify/tart/native.go:107`), three sync the file, one syncs
  the parent, and one checks the context before renaming. The directory form also closes the
  window at `portindex/index.go:371-374` where the old generation is removed before the new
  one is renamed in.
- **A shared exec helper.** Git (`git/repository.go:71-102`), Tart (`tart/client.go:31`),
  launchd (`tart/host/launchd.go:20`), portindex (`index.go:337` and `:138`), and the
  dependency generators (`dependency/generate.go:31`) each run processes with their own copy:
  wait delays of one second, two seconds, or none; different error wrapping; inconsistent
  context joining. One `Run(ctx, Spec)` returning the existing `CommandError` shape would
  serve them all. Per-tool environment policy must stay per-tool; the git environment
  blocklist is a security boundary.
- **A poll helper.** Five fixed-interval `check, select{ctx, timer}` loops
  (`filelock.go:47`, `git/branch.go:73`, `proc/manager.go:30-53`, `tart/host/exec.go:39-54`,
  `tart/host/launchd.go:87-108`). Least important of the four.

## Uncertain external effects

Push, PR create, provider submission, and resource release share one shape: freeze a
precondition, observe first, apply, re-observe and classify. They are encoded four ways:
a bool in the job (`Prepared.IntegrationStarted`, `preparation_integrate.go:52-55`), two
bools and a counter on the publication action (`PushStarted`, `WriteStarted`,
`WriteRefusals`, `publication_run.go:162,256`), an enum plus a durable `Submission` with a
monotonic sequence (`verification_record.go:56`), and `ResourceUncertain` with an idempotent
release (`cleanup.go:130-155`). `git/refs.go:14-16` has the most complete version, with a
conflict-versus-uncertain error taxonomy. `forge.PullRequestInput.ActionID` is threaded but
unused by the GitHub client (`forge/github/pullrequests.go:102`); correlation is by head
branch, not an idempotency key.

Direction: share the vocabulary, a precondition and uncertain error taxonomy, not the
control flow. GitHub's push-and-correlate and Tart's clone-and-launch have different
uncertainty domains, an unowned shared remote run versus a locally owned VM, and one
function for both would obscure that.

## Not findings

- `advanceJob` should not be forced through a generic step runner. Its per-attempt loop
  and provider recheck (`verification_run.go:98-131`) are genuinely different, and a shared
  abstraction would be worse than the duplication.
- `normalizeSpec` should stay one gate. Per-action validators would duplicate the build,
  verification, and destination cross-rules several times.
- `updateExecution`'s reflect-based dirty tracking should not be generalized to more record
  types. The submission close-before-insert ordering at `execution.go:119-129` shows the
  rules are already subtle.
- `git/correction.go:69` uses git's own `index.lock` protocol, not advisory flock; unifying
  it would break interoperability with git.
- `filelock.Acquire` creates paths and `TryExisting` never does; that is a deliberate policy
  split for previews and maintenance.
- `portfile`, `distfiles`, `dependency`, and `source` are well-factored with real types and
  accurate overviews. `assessment.go`'s typed findings and the plan-then-apply split for
  archives are fine.
