# Observing pull requests beyond their state

Roadmap item 6 asked for PR head, mergeability, review, CI, and conflicts to be observed through the existing contribution lifecycle, without authorizing corrective action.

## Design

`record.PullRequestStatus` holds one forge report about a PR head: draft state, `Mergeable` as yes, no, or unknown with the forge's finer detail (clean, dirty, blocked, behind, unstable), `Review` as approved, changes-requested, or none with approval and change-request counts, and a `CheckSummary` counting passed, failed, and pending checks with the failing names. `Summary` renders it in one line. The status is stored on `record.PullRequest` inside the existing JSON observation column, so no migration is needed.

`forge.PullRequestInspector` is an optional interface. `forge/github` implements it with four reads: the PR itself for mergeability and draft, the reviews list reduced to the latest approval or change request per reviewer (a dismissal drops that reviewer), the check runs for the head commit, and the combined commit status for legacy contexts. `RefreshContribution` inspects an open PR when the forge is an inspector, keeps the previous status and reports "status unavailable" when inspection fails, skips closed and merged PRs, and appends the summary to its detail. `status` prints the line with its inspection time. Nothing reads the status to decide anything.

## Validation

- A GitHub adapter test drives all four endpoints with a fake server and checks the reduction: a reviewer's later approval supersedes their change request, a dismissed approval does not count, in-progress runs are pending, and failing check runs and statuses are named.
- A workflow test refreshes a published contribution with a fake inspector: the status is recorded with the PR and shown in the detail, a failing inspection keeps the previous status without failing the refresh, and a merged PR is not inspected.
- Live, an opt-in test (`DOCKHAND_TEST_GITHUB_PR=macports/macports-ports#34715`) inspected an open upstream PR: "mergeable: unknown; review: none; checks: 3 passed, 0 failed, 0 pending of 3". The four recorded contributions in the development state had all been merged, so `refresh` retired them and, correctly, inspected none.
