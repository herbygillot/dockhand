# GitHub verification

`--provider github` verifies a committed branch through GitHub Actions on your personal fork of `macports/macports-ports`. Selecting it authorizes pushing the candidate branch to that fork. Opening an upstream PR is a separate action, requested with `--publish` or the `publish` command. Tart remains the default provider.

```sh
# Prepare an update, push it to your fork, and wait for its workflow.
dockhand bump croc --provider github --wait

# Continue through an upstream PR after workflow success.
dockhand bump croc --provider github --publish --wait

# Verify a committed contribution you edited yourself.
dockhand verify croc --branch my-update --provider github --wait

# Use a different local remote for your personal fork.
dockhand verify croc --branch my-update --provider github --remote personal --wait

# Resume an accepted job. Its recorded provider and destination are retained.
dockhand wait --branch my-update
```

The push destination defaults to `origin`. Dockhand uses publication's destination resolver, verifies that the authenticated GitHub user owns the fork, and requires its parent to be `macports/macports-ports`. The upstream base is `master`. Enable the fork's existing `.github/workflows/main.yml` in GitHub Actions before using this provider. Dockhand neither installs a workflow nor changes repository settings. Git uses its configured push credentials; Dockhand's existing authentication supplies the GitHub API credential. Reading runs requires access to Actions; Dockhand does not require Actions write access to stop tracking a job.

## Coverage

The provider uses the candidate commit's existing MacPorts workflow, which runs on pushes to branches other than `master`. It validates the supported static macOS matrix shape and requires a successful terminal workflow plus successful jobs for every matrix entry. Missing, duplicate, skipped, canceled, or unsuccessful matrix jobs cannot establish a passing workflow.

The accepted test policy is `workflow`, selected automatically for this provider. The MacPorts workflow may tolerate individual port test failures. A successful workflow is recorded as success under that policy; it does not certify that all declared tests passed, every subport built, or every requested port phase ran. Status and PR bodies retain the workflow URL, exact run attempt, and job outcomes. They do not invent compiler versions, image identities, or successful `port` commands. The local platform remains the Portfile evaluation context; the recorded remote job matrix describes execution coverage.

The first version requires one commit changing one selected port directory above an upstream base. At least one added or modified `Portfile` or `files/` entry must match the workflow's Git `AM` filter; deletion-only and rename-only edits do not qualify. All changed paths, including deletions, remain subject to the single-port scope check. It accepts the standard workflow matrix and push filters. The workflow controls subport eligibility, default variants, runners, and dependency installation. Explicit variant overrides, `--tests declared`, `--tests skip`, `--image`, `--capacity`, and `--from-source` are refused with this provider. Working-tree snapshots are not pushed: `verify` requires `--branch`. Preparation commands create committed branches themselves.

## Recovery and branch updates

Before pushing, the provider records its immutable request, destination, workflow ID, expected remote head, and expected matrix in SQLite. It uses short transactions plus per-request and per-remote-branch operation locks. Publication and verification share Git's compare-and-swap push implementation and remote-branch lock namespace. Network operations do not hold SQLite transactions.

A push may complete before GitHub exposes its run. Dockhand retains an uncertain submission and looks up the exact repository, workflow, push event, branch, and commit. A repeated confirmed push is a no-op; recovery does not dispatch or rerun a workflow. Once found, the GitHub run ID and attempt number are pinned. GitHub owns its execution queue; queued runs count as admitted without occupying a Tart slot. `wait` and `start` route recorded jobs to their providers even when both kinds coexist.

The provider refuses to replace an existing different contribution head. After corrective edits or a rebase, reconcile and push the branch with Git, then run `verify --provider github` again. A newly accepted verification inspects GitHub again instead of automatically reusing an older local pass. To request another remote execution of the same commit, rerun the workflow on GitHub first, then verify again. `--fresh` does not dispatch reruns and is currently refused for this provider.

A missing run remains uncertain because a successful Git push does not prove whether a delayed Actions event will execute. This includes pushes that did not trigger a workflow. Dockhand does not repeatedly manufacture new branches or mutate commits. Canceling an uncertain submission closes its local tracking and prevents later submission calls from pushing; a push already sent may still trigger Actions. Inspect the fork's Actions settings and event history when a run does not appear. Canceling an admitted job durably stops Dockhand tracking that request; it does not cancel the remote Actions run, which may be shared by other requests or have been started independently. Subsequent observation reports a local cancellation without inventing a remote conclusion. Cancel the remote workflow in GitHub if that is also desired. No branch or workflow run is deleted during cleanup. Cancellation can wait for a provider operation already holding the request lock; a push already in flight cannot be undone.

## Logs

`--trace` waits for completion and retrieves the pinned attempt's completed job logs through GitHub's API. It does not stream a running job's output. Logs are cached beside the database in `github-verification`; status retains links to the jobs even if log retrieval fails or GitHub later expires its logs. These caches currently require manual removal when no trace reader is using them; VM resource pruning does not manage them.

No live fork push, Actions build, or PR creation was performed while implementing this provider. Automated coverage uses temporary Git repositories, SQLite, simulated workflow observations, and an HTTP test server for the GitHub SDK adapter.
