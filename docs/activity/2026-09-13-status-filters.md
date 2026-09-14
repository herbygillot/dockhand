# Focused status views

## Scope

Committed verification-target inference as `f4d2408`. Implemented focused status selection, taking up Claude's observation that repository-wide output becomes difficult to use as history grows.

## Implementation

- Added `status <job_id>`, `status --active`, and `status --branch <branch>`. Active filtering can combine with either selector; job ID and branch selection cannot combine.
- Kept execution scopes separate from status filters. The existing `Engine.Status` API remains available to attachment and driver callers; `FilteredStatus` shares its projection and read transaction.
- Select matching jobs in SQLite before decoding their details. Branch matches the recorded contribution association, including closed contributions. Active means queued or active, including delayed retries and capacity waits.
- Read related records in bounded job batches, deduplicate shared contributions/revisions, and preserve deterministic ordering. Job selection, attempts, publication, revisions, PRs, and resources come from one snapshot.
- Preserve original evidence references when verification was reused without including the original job's resources. Empty filtered output includes its selection in human and JSON output. Unknown or foreign job IDs report not-found, including when no database or repository registration exists.
- Keep status read-only: no initialization, migration, repository registration, Git branch resolution, provider calls, or workflow advancement. The command still locates the selected checkout's Git common directory to determine repository identity.

All new code and tests were authored for v2. No v1 comments or tests were copied. No new package, dependency, or database migration was required.

## Validation

Focused tests pass for queued/capacity-waiting/terminal selections, recorded branch associations versus standalone checkout provenance, original-attempt reuse, repository isolation, malformed excluded records, closed contributions, and unknown jobs. A 260-job fixture exercises pagination and deduplication of shared changes, current/input revisions, and PRs. A writer changes state between selection and detail reads to verify that the result stays within one snapshot.

CLI tests use real Git and SQLite, remove a recorded branch's Git ref, and verify that job/branch/active views still return recorded data without executing the queued job. Invalid selectors are rejected before repository access. App tests cover missing databases and unregistered repositories without creating state.

The full `make test-race` suite, `make vet`, `CGO_ENABLED=0 make build`, and `git diff --check` passed. The built `dockhand status --help` shows the optional job ID and both filters. No real VM or remote publication was needed.

## Limits

`--active` excludes terminal needs-attention and failed jobs, even if resource cleanup remains outstanding; those remain visible in unfiltered status or a selector covering them. Branch filtering does not infer associations from source-branch names or standalone verification provenance. Branch matching includes recorded history for that name, not a check against the current Git ref.

Filtering reduces data loading and output; the existing repository indexes still require examining repository-local rows for state/branch predicates. This is not a claim of constant-time historical lookup. No new indexes or migrations were introduced for this command slice. Branch selection for `wait` and `cancel` remains future work.
