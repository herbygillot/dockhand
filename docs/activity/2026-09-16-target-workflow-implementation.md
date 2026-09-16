# Target workflow implementation — 2026-09-16

## Recorded outcomes

Added input-version observations to newly bound jobs and distinguished standalone verification from a prepared update in status and progress. Legacy records retain their original evidence; missing version observations are not inferred from today's checkout. Candidate commits are no longer labeled as existing branches before integration is confirmed. Failed preparation and already-current results explicitly report that no update branch/build was produced. Removed duplicated subport names from target labels.

Added regression fixtures for the reported Terraform failure followed by a passing unchanged-checkout verification, and interrupted branch integration. Status remains a read-only projection. No user database or ports-tree edits were made.

Validation: workflow suite passed; focused status and evidence-reuse CLI regressions passed. The initial CLI run exposed one expected assertion on the old success wording, which was updated to assert standalone provenance.
