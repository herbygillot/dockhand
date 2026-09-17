# Interface pass, step 4: the contribution projection

Per the [output design](../output.md), `status` is now contribution-centric.

## The projection

`workflow.Project` (`internal/workflow/contribution_view.go`) turns a `Status` snapshot into one `Contribution` per tracked change, plus one per standalone verification: port, targets, branch, the change ("1.7 -> 1.8.1", "revision bump", "checksum refresh", "verification"), phase, state, next, the PR URL, the one active job with its last detail, the job history oldest first, the last activity time, and whether the change is retired. It is a pure function of the snapshot, so the plain output, the JSON result, and the coming live table read the same words.

The state and next words are derived beside the projection rather than in the CLI's progress formatting: a queued job waits for a driver; an active verification is "building on macOS 26" or "waiting for capacity" from its attempts; a completed job is "branch ready", "verified", "published", or "current"; failures name the failing phase; needs-attention counts the rejected patches; a merged, closed, or abandoned change says so and what was cleaned up or preserved; an open PR reads its inspected status as "PR open, 3 checks pending, approved". Where the next step is a command, the row says it: "publish when ready: dockhand publish jq".

## The command

Plain `status` prints a table with PORT, CHANGE, PHASE, STATE, NEXT, and PR columns; `-v` prints the previous full record with identifiers and times. `status --json` returns `workflow.Overview`: the snapshot's fields as before with `Contributions` beside them, so existing consumers keep decoding the snapshot and new ones read the projection.

## Friction items closed

From the [new-user exercise](../reviews/2026-09-16-new-user-deno-exercise.md): item 22(a) for `status` (the ID soup) and 22(b) (the empty-field "Change" line), both now behind `-v`. Item 20's empty-database output is unchanged.
