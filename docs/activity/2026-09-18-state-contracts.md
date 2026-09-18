# State contracts: the observation schedule and operation-shaped reads

From the [state-boundaries review](../reviews/2026-09-17-workflow-state-boundaries.md), the two items left after durable branch cleanup.

## The schedule lives on the pull request

Periodic PR observation was throttled by a process-local map keyed by repository and change: two drivers polled the same PR, and a restart forgot every throttle. The pull request record now carries `ObserveAfter`, the earliest time a cycle looks again, in a column of its own (schema 21). Every recorded observation sets it a full interval ahead, whether an explicit `refresh` or the cycle made it, so the two share one recording path; a failed look leaves the last observation standing and pushes the next look out the same way, on the record; an explicit refresh ignores the schedule and resets it from its own observation. The cycle asks the store for the open contributions whose look is due, longest waiting first, a few at a time, instead of reading every change and filtering. A second engine over the same store, standing in for another driver or a restart, leaves a PR alone within its interval; the test says so.

## Reads shaped like operations

`state.Query` stays as the generic read for status and selection, a filter over one record kind ordered by ID and paged with `After`. Its one hazard, a due-ordered result paged by ID, which can skip a record whose due time moved, is now refused as invalid rather than avoided by not paging. Beside it the reader has the reads the driver actually makes, each bounded and ordered for its operation:

- `DueJobs`: jobs whose next action is due, soonest first, at most a limit, for the cycle.
- `JobHistory`: one contribution's jobs newest first, for the newest-job lookups that selection and acceptance make; they filter by action or build in Go over a short list instead of asking the store for a filtered newest one.
- `OpenContributions`: open contributions with a due PR, for observation.
- `OwedCleanups`: merged contributions with a pending cleanup side, for the cycle's housekeeping, which previously read every change.
- `CleanupCandidates`: terminal jobs finished before a cutoff, paged by ID, for the log-cache sweep.

The explicit working-set change set the review also asked for is a maintenance hazard rather than an incident and is still deferred.

## Tests

`sqlite`: the five reads' ordering, bounds, and selections, the schedule's round trip, and the refused query combination. `workflow`: the observation schedule survives a second engine over the same store, an explicit refresh looks at once and resets it, and a failed look waits a full interval on the record; the retirement, cleanup, and retention tests pass unchanged. The whole suite passes.
