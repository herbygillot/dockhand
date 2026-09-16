# State, backups, and retention

Dockhand's SQLite database is an operational asset. It holds every registered repository, accepted request, job, contribution revision, verification result, resource obligation, provider reservation, and publication checkpoint. Git does not contain a second ledger from which this database can be rebuilt. One database coordinates all drivers using a shared Tart pool.

## Back up and check the database

```sh
dockhand db backup "$HOME/Backups/dockhand-2026-09-13.db"
dockhand --db "$HOME/Backups/dockhand-2026-09-13.db" db check
```

`db backup <file>` includes all repositories, not just the current checkout. It works while other processes use the database and includes committed data still in the WAL. The destination must not exist. Dockhand writes a private temporary snapshot, checks it, syncs it, then installs the complete file atomically without overwriting another file. The destination filesystem must support hard links. These commands need neither a Git repository nor MacPorts, Tart, or GitHub access. They also accept older supported Dockhand schemas without upgrading them. An absent database is an error, not an invitation to initialize one.

The SQLite implementation uses [VACUUM INTO](https://www.sqlite.org/lang_vacuum.html#vacuum_with_an_into_clause) for a consistent snapshot and checks both [database integrity and foreign keys](https://www.sqlite.org/pragma.html#pragma_integrity_check). An ordinary copy of a live `state.db` alone can omit committed WAL data; use `db backup` instead. Keep backups on storage appropriate to the failures you want to recover from. No automatic backup schedule is installed.

The backup is one standalone database file. It does not include Git objects, source checkouts, VM images, live VMs, build logs, or remote pull requests. Those resources have independent lifetimes. `db check` checks database structure and references; it neither repairs damage nor validates those external resources.

## Recover from a backup

Recovery restores a point in time, not the current state of external systems. Stop every Dockhand driver using the affected database before changing its operational file. Keep the original database and its WAL/SHM files together for investigation; do not delete sidecars as a repair technique. Do not run a backup and the original as separate coordinators for the same Tart pool.

Check the backup and create a replacement at an unused name in the original database directory:

```sh
dockhand --db "$HOME/Backups/dockhand-2026-09-13.db" db check
dockhand --db "$HOME/Backups/dockhand-2026-09-13.db" \
    db backup "$HOME/.dockhand/recovered.db"
dockhand --db "$HOME/.dockhand/recovered.db" status
```

Run `status` from each affected checkout. It reads records without resuming work. With an older-schema snapshot, use the matching Dockhand version for inspection; normal writable opening upgrades supported schemas when work resumes. Keeping the replacement beside the original preserves default artifact and publication-lock locations. Repository identities still refer to their recorded Git common directories, and provider payloads retain their original paths.

Before resuming, compare recorded jobs and resources against current Git branches, Tart VMs, and PRs. A backup can predate a VM admission, remote push, PR write, or artifact deletion. Existing reconciliation handles interruptions whose identities and checkpoints survived; it cannot guarantee safe replay of effects that occurred after the snapshot. Inspect those gaps explicitly. Unrecorded VMs must be identified and resolved before treating pool capacity as available. An uncertain PR write must be checked against GitHub before any new publication attempt. Merely waiting for an old claim to expire does not reconstruct missing history.

After that reconciliation, use the replacement as the sole operational database through `--db`, or replace the default path while all drivers remain stopped and the original files are preserved. Restoring/overwriting the live database and repairing lost external-operation history are deliberately not automatic commands. Without a usable database or backup, verification evidence, accepted intent, ownership, and publication checkpoints cannot currently be reconstructed from the repository alone.

## Routine cleanup

Ordinary driver cycles release terminal verification VMs, including failures, after collecting their result and available build logs. `bump`, `bump-revision`, `refresh-checksums`, `verify`, `amend`, and `rebase` accept `--keep-failed` to retain failed local environments for investigation. That choice is stored with the accepted job and survives driver restarts. Successful and canceled runs are released even when the flag was set. It does not change verification evidence compatibility. Existing retained environments are not retroactively released by this policy change.

Tart removes its host `input.tar` after guest staging and durable admission; retries launch the same staged guest without retransferring the archive. Interrupted staging keeps its input until the reserved execution is closed and released. Normal preparation/staging scratch remains scoped to the operation and is removed on return, including errors. Unknown leftovers from process death are not swept blindly.

Host logs and result files survive VM deletion. Once both the job and resource release are at least seven days old, a cycle may prune up to eight released diagnostic directories in the current repository. It skips active work, live claims, and recorded build outputs. Provider locks serialize removal outside database transactions, and failed/unavailable pruning backs off for a day rather than blocking every cycle. Compact evidence, history, branch/PR identities, and small coordination files survive. No extra daemon is started; cleanup resumes with later driver activity.

`--keep-failed` retains the VM until explicit `gc` or a recorded retention deadline releases it; its logs remain for at least another seven days after that release. Reusable indexes and re-downloadable GitHub log caches keep their separate explicit `gc` policy below.

## Reclaim environments and diagnostic files

```sh
dockhand gc --dry-run
dockhand gc
dockhand gc --older-than 720h --dry-run
```

`gc` operates on the current checkout's registered repository. Its default age is seven days (`168h`); durations use Go syntax such as `24h`, and `--older-than 0` explicitly includes recent terminal work. Dry-run uses read-only state and contacts no provider. Missing state or an unregistered repository yields no eligible cleanup.

There are two independent age checks:

- A terminal job older than the threshold can have its retained VM released. Future explicit retention deadlines and live claims are respected. Release uses the same ownership checks, claim, provider call, and confirmation as an ordinary driver cycle.
- Diagnostic files can be removed only after both the job and the confirmed resource release are older than the threshold. This preserves logs for another retention interval after releasing a failed VM. Removal includes the released run's host directory and logs, but skips resources with recorded build outputs.

Active jobs, unresolved attempts, uncertain ownership, and unregistered directories are not discovered by scanning the filesystem for garbage. Cleanup starts with owned resource records. Normal cycles release terminal VMs by default. Environments explicitly kept for investigation, and failures already retained by older versions, remain retained until `gc` or a recorded deadline makes them eligible.

Release failures remain durable cleanup obligations and can be retried by `gc` or another driver cycle. File-pruning failures are reported by `gc` or the cycle and remain retryable. Explicit `gc` may retry released diagnostics immediately, without waiting for automatic pruning backoff. A filesystem removal followed by a failed database checkpoint is also safe to retry. `gc` reports each action and returns an error if a selected action remains incomplete. It never advances unrelated jobs.

Status records the time diagnostics were pruned. Historical log paths remain in immutable evidence, and reading a missing terminal log reports that it is unavailable. Verification verdicts, input identities, job/request history, PR associations, provider results, and closed submission identities are retained. Pruning logs does not invalidate a verification result.

Per-submission and publication lockfiles remain in place. Unlinking a lock while another process holds or awaits its inode can create two independent locks for the same operation. Closed provider identities also prevent delayed submissions from creating a second run. Reclaiming these small records would require a separate retirement protocol; deleting the large released directories provides the useful space savings now.

## Index and GitHub log cache retention

`dockhand gc --dry-run` previews shared PortIndex entries and local GitHub log caches alongside retained environments and released Tart diagnostics. `--older-than` defaults to seven days. Cache entries must be older than that threshold since their last use; GitHub logs also require an old terminal job and terminal attempt without a live claim or retained build outputs.

Index cleanup covers the shared `dockhand/indexes` cache in the user cache directory and, for databases created before consolidation, the legacy `indexes` cache beneath the selected Tart artifact directory. Both are disposable and shared across repositories. Collection takes each indexing environment's exclusive lock, so environments with an active reader or builder are skipped; it removes generations and leftover build directories older than the threshold, keeps the environment's latest seed regardless of age, preserves lockfiles and unknown paths, and never removes the staged index already copied into a build. Cache entries are regenerated when needed. A queued request does not pin a disposable index.

GitHub log cleanup covers the current registered repository's aggregate logs and completed per-job download parts. It uses each request's existing lock and skips active downloads. It preserves database results, evidence, run identities, and remote log URLs; it does not delete anything on GitHub or require authentication. An explicit later log read may download the cache again if GitHub still retains the logs. Previews do not create directories/lockfiles or contact remote services. As with other repository garbage collection, an absent database or unregistered checkout has no eligible cleanup.

Use the preview to see exact cache paths and attempt IDs. A concurrent run may make an entry busy or refresh its last-use time between preview and collection, so the actual set can differ. Incomplete cache build directories and orphaned unidentified GitHub download temporaries are left alone in this initial retention pass.
