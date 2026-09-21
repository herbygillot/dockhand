# GitHub verification

`--provider github` verifies a committed branch through GitHub Actions on your personal fork of `macports/macports-ports`. Selecting it authorizes pushing the candidate branch to that fork. Opening an upstream PR follows by default; `--to verified` stops after the workflow, and the `publish` command does it later. Bumps default to `--provider auto`: Tart is preferred when its matching prepared image is available; missing Tart or an unavailable suitable image selects GitHub instead. Standalone `verify` still defaults to Tart. Explicit provider choices take precedence.

```sh
# Prepare an update, push it to your fork, wait for its workflow, and open the PR.
dockhand bump croc --provider github

# Stop after workflow success without a PR.
dockhand bump croc --provider github --to verified

# Verify a committed contribution you edited yourself.
dockhand verify croc --adopt my-update --provider github

# Use a different local remote for your personal fork.
# The fork is found by URL and login; name it only when two remotes qualify:
dockhand verify croc --adopt my-update --provider github --remote personal

# Resume an accepted job. Its recorded provider and destination are retained.
dockhand wait --branch my-update
```

The push destination defaults to `origin`. Dockhand uses publication's destination resolver, which already refuses a push repository the authenticated GitHub user does not own, and additionally requires the fork's parent to be `macports/macports-ports`. The upstream base is `master`. Enable the fork's existing `.github/workflows/main.yml` in GitHub Actions before using this provider. Dockhand neither installs a workflow nor changes repository settings. Git uses its configured push credentials; Dockhand's existing authentication supplies the GitHub API credential. Public reads, including bump previews, resolve the same available credentials; only a missing credential permits anonymous access. An existing Dockhand Keychain credential takes precedence over `gh`; if it is rejected, replace it with `dockhand auth login`, remove it with `dockhand auth logout` to use `gh`, or explicitly override it with `GH_TOKEN`. Dockhand does not silently try another credential after rejection. Reading runs requires access to Actions; Dockhand does not require Actions write access to stop tracking a job.

## Coverage

The provider uses the candidate commit's existing MacPorts workflow, which runs on pushes to branches other than `master`. It does not pattern-match that workflow's shape. A contribution is one commit confined to a single port directory above a base present in upstream master, so the file that will run is upstream's own, and how many jobs it declares, what they are called, and which runners it names are MacPorts' decisions to make. Dockhand reads the file for one thing before pushing: a push trigger whose branch filters would exclude the branch about to be pushed is refused, because that run would never exist.

The verdict comes from the run. A passing workflow requires a successful terminal run that carried at least one job, every job concluded successfully, no job name repeated, and every job that reported its `runs-on` labels having run on a macOS runner. When the workflow also named its jobs after its runner matrix, which is what makes that prediction match GitHub's own job names, the run is held to that list as well. A blocked verdict names which of these failed.

The provider's test policy is its workflow's, recorded as `workflow`; `--tests` is a Tart option and is refused with `--provider github`. The MacPorts workflow may tolerate individual port test failures, and Tart's default `declared` policy tolerates them the same way; only `--tests required` on Tart makes them decisive. A successful workflow is recorded as success under that policy; it does not certify that all declared tests passed, every subport built, or every requested port phase ran. Status and PR bodies retain the workflow URL, exact run attempt, and job outcomes. They do not invent compiler versions, image identities, or successful `port` commands. The local platform remains the Portfile evaluation context; the recorded remote job matrix describes execution coverage.

The first version requires one commit changing one selected port directory above an upstream base. At least one added or modified `Portfile` or `files/` entry must match the workflow's Git `AM` filter; deletion-only and rename-only edits do not qualify. All changed paths, including deletions, remain subject to the single-port scope check. It accepts the standard workflow matrix and push filters. The workflow controls subport eligibility, default variants, runners, and dependency installation. Explicit variant overrides, `--tests declared`, `--tests skip`, `--image`, `--capacity`, and `--from-source` are refused with this provider. Working-tree snapshots are not pushed: `verify` requires `--branch`. Preparation commands create committed branches themselves.

## Recovery and branch updates

Before pushing, the provider records its immutable request, destination, workflow ID, expected remote head, and expected matrix in SQLite. It uses short transactions plus per-request and per-remote-branch operation locks. Publication and verification share Git's compare-and-swap push implementation and remote-branch lock namespace. Network operations do not hold SQLite transactions.

A push may complete before GitHub exposes its run. Dockhand retains an uncertain submission and looks up the exact repository, workflow, push event, branch, and commit. A repeated confirmed push is a no-op; recovery does not dispatch or rerun a workflow. Once found, the GitHub run ID and attempt number are pinned. GitHub owns its execution queue; queued runs count as admitted without occupying a Tart slot. `wait` and `serve` route recorded jobs to their providers even when both kinds coexist.

A managed correction replaces what it corrects. The push refuses to disturb a fork branch whose head is neither the candidate, the contribution's base, nor a head the correction named, and a correction names one: the pull request's head when the contribution is published, and otherwise the commit being corrected, which a previous verification of this contribution is what put there. The provider still compares before it writes, so a branch holding anything else is left alone and reported rather than replaced.

Once a PR exists, the provider pushes to and watches the PR's recorded head branch even after the local branch is renamed; the local name only says which commit to capture. The provider refuses to replace an existing different contribution head. After corrective edits or a rebase, reconcile and push the branch with Git, then run `verify --provider github` again. A newly accepted verification inspects GitHub again instead of automatically reusing an older local pass. To request another remote execution of the same commit, rerun the workflow on GitHub first, then verify again. `--fresh` does not dispatch reruns and is currently refused for this provider.

Known pre-push failures such as a missing local branch, unsuitable or already-merged base, rejected authentication, or unavailable workflow reference stop with a durable reason. Temporary network/service and rate-limit failures remain retryable. A rejection recovered after an interrupted driver does not create another submission.

A missing run remains uncertain because a successful Git push does not prove whether a delayed Actions event will execute. This includes pushes that did not trigger a workflow. Dockhand does not repeatedly manufacture new branches or mutate commits. Canceling an uncertain submission closes its local tracking and prevents later submission calls from pushing; a push already sent may still trigger Actions. Inspect the fork's Actions settings and event history when a run does not appear. Canceling an admitted job durably stops Dockhand tracking that request; it does not cancel the remote Actions run, which may be shared by other requests or have been started independently. Subsequent observation reports a local cancellation without inventing a remote conclusion. Cancel the remote workflow in GitHub if that is also desired. No branch or workflow run is deleted during cleanup. Cancellation can wait for a provider operation already holding the request lock; a push already in flight cannot be undone.

## When a pushed branch has no visible run

Status identifies the accepted fork, branch, commit, and submission timestamp, then distinguishes a confirmed push from a remote-branch conflict. When no matching run is found, the driver also reads the current `main.yml` reference: active, disabled, replaced, or missing/inaccessible. A workflow URL comes from GitHub when available. These observations do not change the accepted workflow ID or establish that an earlier event cannot still execute. Transient API errors continue through the normal retry/backoff path.

Use the exact job ID shown by status:

```sh
dockhand wait <job-id> --trace
# Or explicitly stop tracking this job, including when GitHub is offline:
dockhand cancel <job-id> --wait
```

A delayed matching run can still be adopted after a restart or workflow-settings change. There is no elapsed-time failure cutoff. If the remote branch has moved, Dockhand keeps looking for the accepted commit's run without replacing the unexpected remote head. Cancellation closes only this request's tracking; the branch remains and remote Actions may still execute. A late run cannot revive canceled tracking.

Inspect the fork's Actions page and correct workflow access/settings or the push setup before requesting new work. Enabling a workflow alone does not cause Dockhand to replay the earlier push. Reconcile and push an eligible contribution revision with Git, then use `verify --branch <branch> --provider github`. `wait` resumes observation; it does not dispatch a workflow of its own. Managed branch replacement remains separate design work.

A run for the accepted commit that has finished without succeeding is run again rather than adopted. The engine asks this provider for a verdict only when recorded evidence did not satisfy it, so answering with a conclusion already reached would answer with nothing; the unsuccessful jobs are run again, which adds an attempt to the same run rather than starting another, and that attempt is the one observed. Only the legs that did not succeed are repeated, so retrying a matrix costs one runner rather than all of them, and earlier attempts stay readable, so a job watching one keeps its verdict. Asking happens once for a request: a run that accepted a rerun is no longer finished, and one that refused, because its logs expired or it has no unsuccessful job to repeat, is observed with the conclusion it reached. A run still going, or one that succeeded, is left alone. Nothing here happens on a driver's initiative; it happens when someone asks for a verification.

## Logs

`--trace` waits for completion and retrieves the pinned attempt's completed job logs through GitHub's API. It does not stream a running job's output. Logs are cached beside the database in `github-verification`. Completed caches can be read without resolving credentials or contacting GitHub. Each completed job download is retained if a later download fails; retrying resumes with the missing jobs, then assembles the log in a stable order and removes the individual caches. Partial job downloads are discarded and retried. Status retains links to the jobs even if log retrieval fails or GitHub later expires its logs. These caches currently require manual removal when no trace reader is using them; VM resource pruning does not manage them.

Automated coverage uses temporary Git repositories, SQLite, simulated workflow observations, and an HTTP test server for the GitHub SDK adapter. The [xplr live exercise](activity/2026-09-15-xplr-github-exercise.md) covers fork push, hosted builds, PR publication, driver recovery, shared-run tracking cancellation, and rerun identity.

Automatic selection happens once during bump intake and records a concrete provider. Tart capacity waits and failed local builds do not cause GitHub submission. Publication can still trigger the repository's own push/PR Actions independently of Dockhand's verifier. Explicit local image, capacity, variant, source, or local test-policy options retain Tart rather than silently discarding those choices. If automatic fallback cannot configure GitHub, preparation can still complete and preserve the branch, but verification requires attention; an already-current bump can finish without verification. Explicit GitHub configuration errors remain intake errors.
