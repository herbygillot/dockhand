# Contribution lifecycle

Implemented the first roadmap item: explicit local `abandon` and associated-PR `refresh`. Existing workflow selectors and state records supply identity and concurrency; no new package, table, or background monitor was needed. CLI help, usage, and the roadmap now describe the completed slice.

Abandonment excludes pending jobs transactionally and preserves Git work, evidence, and remote PRs. Refresh observes outside transactions, fences stale observations against both contribution and PR snapshots, and checks the local branch under its operation lock. Terminal remote outcomes retire only idle work matching its published revision. Newer revisions, dirty/moved branches, changed remote heads, and reopened historical PRs cannot redirect or discard current work. The GitHub adapter accepts terminal PRs whose fork has been deleted while retaining the previously known locator.

## Validation

- Workflow, CLI, forge, and SQLite regression suites passed. Targeted lifecycle, concurrency, and GitHub parsing tests passed under the race detector.
- Tests cover branchless failed preparation, close-versus-retry races, active jobs, missing/moved/dirty branches, newer revisions, closed/merged/reopened PRs, deleted forks, and concurrent refresh/abandonment.
- Built `/private/tmp/dockhand-roadmap`. An isolated shared clone and a SQLite backup were used; only the backup's repository locator was adjusted. No original state, branches, VMs, or PRs were changed.
- Live refresh of existing MacPorts PR #34665 observed `merged` and retired its matching contribution in the copy.
- Abandoned copied failed Terraform preparation, then ran `bump terraform-1.16 --no-verify --json`. A new contribution fetched master `0f8e26f480b8a6f0fd39ea57c58f2083e259e06b`, discovered 1.16.3, refreshed both Darwin architecture archives, and prepared branch `dockhand/bump/terraform-1-16-iw2qmkxtobsk432gxgbo3ueeg2`. This exercise intentionally did not verify or publish another PR.

Implementation and tests were authored for this change; no v1 code or comments were copied. Broader PR review/CI observation remains separate work.
