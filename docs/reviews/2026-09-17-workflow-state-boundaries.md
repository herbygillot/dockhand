# Review: workflow and state boundaries

Date: 2026-09-17. Reviewed the clean working tree at `c6cf638`. This is a
focused follow-up to the [bump architecture review](2026-09-17-bump-workflow-architecture.md),
covering lifecycle recovery, scheduling, query contracts, and transaction working
sets. These observations are proposals for triage, not accepted implementation work.

The core transaction model remains appropriate: repository-scoped reads, short
writes, explicit claims, and external operations outside transactions. The useful
next concepts are smaller contracts around that model. Five seams stand out.

## 1. Persist branch cleanup separately from contribution retirement

This is the clearest current recovery gap.
[`refreshChange`](../../internal/workflow/contribution_lifecycle.go) commits the
merged disposition before calling
[`retireBranches`](../../internal/workflow/contribution_branches.go). Cleanup
returns human-readable notes, which are appended to the command result rather
than stored as retryable obligations.

If the process exits after retirement, or remote deletion fails, the contribution
is already merged. Periodic PR observation selects only open contributions, and
an explicit refresh of an already-retired contribution returns before the cleanup
path. [`collectMergedBranches`](../../internal/workflow/retention_branches.go)
does recover local leftovers, but explicitly excludes fork branches.

The presentation also maps every merged contribution to "merged; branches cleaned"
in [`contribution_view.go`](../../internal/workflow/contribution_view.go), even
when deletion was skipped or failed. Retirement therefore does not establish the
cleanup fact displayed to the user.

Introduce a small durable branch-cleanup obligation, recorded atomically with
retirement. Track local and remote outcomes independently: pending, complete, or
kept for a stated reason. Retain the expected published commit and destination
identity, and recheck them before deletion. A branch that moved must remain
protected. Reuse the existing claim/retry vocabulary, but keep contribution
branches distinct from provider-owned VM resources.

This would make cleanup recoverable and let status distinguish a merged
contribution from completed housekeeping.

## 2. Give PR observation a scheduler with explicit ownership

[`observePullRequests`](../../internal/workflow/contribution_lifecycle.go) scans
all contributions, filters open ones in memory, and limits work to four
contributions per cycle. Its interval is maintained in the package-global
[`lastObserved` map](../../internal/workflow/engine.go).

This schedule is process-local: separate drivers can poll the same PR, and a
restart loses the throttle. Explicit refresh does not update that map. The limit
bounds selected contributions rather than actual forge calls, because one refresh
can both observe and inspect a PR. These are different semantics from the durable
scheduling used for jobs and resources.

Introduce a PR observation coordinator with persisted next-attempt time and a
short claim when coordination across processes is required. Keep attempt timing
separate from the timestamp of the last successful observation. Let explicit
refresh and periodic observation use the same recording path, with explicit
refresh able to request an immediate look.

Add a bounded query for due, open, PR-associated contributions. This makes the
cycle's cost and scheduling rules easier to understand without turning every
poll into a user-visible job.

## 3. Replace the catch-all query with contracts matching its operations

[`state.Query`](../../internal/state/store.go) serves jobs, changes, revisions,
resources, and controls. Fields have entity-specific meanings; some combinations
are rejected and other fields are ignored by particular implementations.

Pagination exposes the ambiguity. In
[`sqlite.Jobs`](../../internal/state/sqlite/query.go), `After` filters by ID, but
`DueBefore` changes ordering to `(next_action_at, id)`. Due resource queries have
the same mismatch. A caller paging a due-ordered result by the last ID can skip
records: an early-due `z` followed by a later-due `a` cannot be traversed with
`id > z`. The current cycle takes a bounded due batch without that pagination;
this is a contract hazard, not a demonstrated missed-job incident in that caller.

Separate operations such as `DueJobs`, `JobHistory`, `OpenContributions`, and
`CleanupCandidates`, or use small entity-specific query types. A due-work query
can intentionally expose no cursor; a paginated query needs a cursor matching its
sort order. Reject unsupported combinations explicitly while migrating callers.

The existing dedicated `VerificationQuery` is a useful precedent. No general
query builder or ORM is needed.

## 4. Make ownership and writes explicit in the execution working set

[`execution.go`](../../internal/workflow/execution.go) loads a job, its revision,
all attempts, current submissions, and resources into one working set.
`cloneExecution` copies the maps and their struct values, but leaves nested
pointers, slices, and maps shared. `updateExecution` uses `reflect.DeepEqual`
against the original to decide what to persist.

This requires a copy-before-mutation convention that the type does not enforce.
For example, mutating an existing `work.Plan.Targets` element in place also
changes the original plan used for comparison, so the comparison cannot detect
that edit. This review did not establish a current production path losing a write
that way; it is a concrete maintenance hazard in the abstraction.

Prefer an explicit change set or mutation methods that record which entities
changed. Another bounded option is to deep-copy exactly the mutable fields the
API permits callers to edit. Keep submission close-before-replacement ordering
explicit: the database's one-open-submission rule makes write ordering meaningful.

Preserve this as a workflow-specific unit of work rather than expanding it into
a generic persistence framework.

## 5. Separate operational reads from full history reads

Two paths load more than their immediate task needs:

- `loadExecution` loads every attempt and its resources under the write
  transaction even when advancing one attempt. Cohort settlement needs aggregate
  information, but a provider observation does not necessarily need all sibling
  resource payloads.
- [`workflow.status`](../../internal/workflow/status.go) assembles complete job
  history and related records. The live table's default polling interval is two
  seconds in [`tui/status.go`](../../internal/tui/status.go), and its
  [poll callback](../../internal/cli/status.go) requests that status projection.

The backend first selects IDs and then reads each record individually, so both
history size and cohort size can multiply query work. This is a structural scaling
concern; no new latency measurement was made during this review. Short SQLite
write transactions remain especially important because writers serialize.

Introduce a compact contribution-summary query for the live view and load history
when requested. For advancement, consider an attempt-sized working set plus
cohort completion facts, preserving atomic settlement. Batch record retrieval
inside the backend where measurements show it matters; callers should not need
to manage SQL joins.

Extend the existing [state performance probe](../../tools/stateperf/README.md)
with large verification cohorts and full live-status reads. Its current documented
cases emphasize historical database size, selected-job status, and contending
drivers. These extra cases would establish whether narrower working sets are worth
the implementation complexity.

## Priority and review limits

Start with durable branch cleanup and accurate cleanup status, then tighten query
contracts. PR observation scheduling is the next useful extraction. Explicit
working-set mutation would reduce future correctness risk; narrower read models
should be guided by measurements.

This pass used source inspection and existing test inspection. No tests or
benchmarks were rerun, and no real builds, publication, or cleanup operations were
performed. The earlier review's successful test run does not constitute validation
of the newer commit. Only this review document was added.
