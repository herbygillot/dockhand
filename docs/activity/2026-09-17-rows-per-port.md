# One row per port; branchless failures retire

Observed by the user in the live table on 2026-09-17: several rows per port, because every bump that failed before creating a branch was still an open contribution, standalone verifications stood beside the port they verified, and retired contributions kept full weight.

## Branchless failures retire

A preparation that ends needing attention with no branch now closes its contribution in the same transaction, through the existing `closeEmptyContribution`, which only ever closes a change with no branch and no revision. There is nothing to pursue in such a contribution, and leaving it open made the next bump either create a peer (when it was found by target, it was not pending) or, one bump later, refuse with "multiple open contributions". The next bump now starts from fresh master. This reverses the earlier rule that a retry rejoined a failed preparation's frozen source; once a branch exists, retries continue the contribution as before. The test that pinned the old rule now pins the new one.

## Rows fold by port

`workflow.Project` keeps one row per port: the newest open contribution, or the newest row of any kind when nothing is open, with the port's other contributions and its standalone verifications under `Earlier`, newest first. The table shows them in the expansion as "earlier: <change>; <state>; <next>". A retired branchless contribution reads "retired: stopped before a branch; bump again once fixed: <reason>", so the reason stays visible without pretending there is something to continue.

The `b` key now runs a plain `bump <port>`, which continues the port's open contribution by itself and starts afresh when the row's contribution has retired; passing `--change` would have refused the retired one.
