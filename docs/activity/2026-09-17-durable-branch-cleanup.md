# Branch cleanup is owed until settled

The first item of the reconciled queue after the setup-argument work, from the [state-boundaries review](../reviews/2026-09-17-workflow-state-boundaries.md) and from the merged branches that hung around earlier the same day.

## What was wrong

`refreshChange` committed the merged disposition and only then called `retireBranches`, whose notes went into the command result and nowhere else. A process exit between the two, or a fork deletion that failed because GitHub was unreachable, left a merged contribution nothing revisited: periodic observation selects open contributions, an explicit refresh of a retired one returned before the cleanup path, and `gc` sweeps local leftovers only. The status view then said "merged; branches cleaned" for every merged contribution regardless.

## What changed

A merged contribution now records what it owes in the same transaction that records the merge: `record.BranchCleanup` on the change, with the published commit both branches are expected to hold and one `CleanupOutcome` per side, local branch and fork branch, each pending, complete, or kept with the reason. The change row gained a `cleanup` column (schema 20) holding it.

Settling is separate from retirement and idempotent. `settleCleanup` attempts the pending sides, deletes only while the branch still holds the published commit, and records each outcome: deleted or already gone is complete; a branch that moved past the published commit, or a fork no local remote pushes to, is kept for that reason; a branch that is checked out, or a deletion that failed for a reason that can pass, stays pending with a retry time that backs off from fifteen minutes, doubling to eight hours, the bookkeeping a `Lease` keeps. The refresh that observed the merge settles at once; an explicit `refresh` of a merged contribution settles whatever is still owed, ignoring the retry time; the processing cycle takes up a few due obligations beside PR observation; `gc`, when it deletes a merged contribution's local branch, settles that side. A side another settler finished meanwhile keeps its outcome. Contribution branches stay distinct from provider VM resources: nothing here touches the resource records.

`status` words the merged row from the record: "merged; branches cleaned" only once both sides are settled, otherwise what is still owed or kept and why, and plain "merged" for a contribution retired before cleanup was recorded, which is what the earlier merged rows now say rather than a claim nothing supports.

## Tests

`workflow`: a merged PR whose fork deletion fails records the fork side pending with a retry time and the local side complete, the cycle leaves it alone until due and settles it after, and the projected row says what is owed and then that the branches were cleaned; an explicit refresh of the merged contribution settles the owed side at once; the existing checked-out-branch case still reports the branch kept and `gc` still sweeps it; the merged row wording covers nil, settled, pending, and kept. `sqlite`: the migration count and the change round trip carry the new column. The whole suite passes.
