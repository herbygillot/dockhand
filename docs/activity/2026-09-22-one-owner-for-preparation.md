# 2026-09-22: one owner for preparation

The first part of Next item 1, from the [reconciliation](2026-09-22-review-reconciliation.md)
of the second architecture review.

## What diverged

A fresh bump was prepared inside the durable workflow: bound, accepted,
its release resolved and checkpointed, its candidate prepared under a
lease, integrated, and gated on the editor's patch findings. An update
onto an open contribution, an adopted branch or one amended by hand, ran
`app.prepareOnto` instead: it resolved the release and ran the editor
synchronously, before acceptance, and bound the prepared tree as a
correction. The correction request had no field for patch findings, so
the patch gate saw none for an amendment; it had no field for
`--all-subports`, so the choice was dropped; and nothing of that work was
checkpointed or leased.

## The change

The engine binds the update. `PreparationRequest.Onto` names the
contribution, and `BindPreparation` reads its current revision as the
source, takes its target as the selection with the person's variants
laid over, takes the contribution's own subject unless one is given,
checks that no other job is pending on it and that an attached pull
request is open, and records a correction spec on the job with no
captured candidate: the revision, the branch, and the heads the
integration expects. The cycle then does what it does for a fresh bump.
The correction fast path applies only to a captured candidate, an amend
or a rebase; an onto job resolves its release and prepares its candidate
under the same lease, and integration replaces the branch head as one
commit on the contribution's base, checking the same preconditions and
the same patch gate. A branch that already holds the update completes
the job with "nothing to amend".

`app.BindPreparation` decides only which path a selection takes: a
contribution with no bump job of its own, or whose last bump was itself
prepared onto it, goes onto; a contribution dockhand prepared from master
continues from master as before. `app.prepareOnto` is gone, and the
subject helper moved to the engine as `ContributionSubject`, which the
preview still uses.

The action rule table says it: a preparing action may carry a correction
without a candidate, and amend and rebase must carry one.

## Proved

A workflow test adopts a tracked contribution, binds a revision bump onto
it with `--all-subports`, and prepares it with an editor that reports a
failing patch: the choice is on the job, the findings reach the gate and
stop verification, the branch head is replaced by one commit on the
contribution's base carrying the contribution's message, and the
contribution's current revision is the amendment. The CLI test of
preparations landing on an adopted branch passes unchanged.
