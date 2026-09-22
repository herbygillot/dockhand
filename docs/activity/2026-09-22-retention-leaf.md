# 2026-09-22: retention is a leaf, and a branch is deleted one way

The third part of Next item 1. The 09-21 review named retention as the
next leaf sub-package of `workflow`, since it reaches the engine mostly
for the store and the repository; the 09-22 review found the merged
branch sweep re-deriving the lifecycle's branch deletion, the checked-out
test, the expected commit, the delete, and the record, each on its own.

`workflow/retention` now holds the collector: the resource action and its
release or pruning, the log-cache sweep, the merged-branch sweep, and the
cleanup outcomes, `RetryLater` and `Settled`. It takes what it needs as
values, the store, the repository, the clock, the git repository, provider
routing, and the engine's claimed release of one resource as a function
that turns a lost claim or a conflict into a detail, and a call timeout.
It imports neither the engine nor the cycle. `Engine.Collect` builds a
collector from its cycle and delegates; the cycle's bounded diagnostic
pruning goes through the same collector.

`DeleteLocalBranch` is the one decision: a merged contribution's local
branch is deleted when it still holds the published commit and is checked
out nowhere, and the outcome is worded once, deleted, already gone, kept
because it moved, or kept for now with a backed-off retry. The lifecycle's
settlement calls it, and the sweep calls it and settles the recorded
obligation when it deleted. The options, item, and result types moved
with the collector, and their users in `app`, `cli`, the GitHub provider's
tests, and the workflow tests name the package; nothing keeps the old
names.
