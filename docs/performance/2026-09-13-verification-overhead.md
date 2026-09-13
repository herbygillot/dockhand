# Verification overhead — 2026-09-13

## Image-validation probe

Measured independent CLI processes on this Darwin 25 arm64 host, using the stopped `dockhand-base-tahoe` image and the local MacPorts ports checkout. Baseline code is commit `8f2b4bc`; the comparison adds the persistent cache and workflow timing changes.

Each invocation used a scratch SQLite database and `verify chezmoi --branch dockhand-perf-missing-8f2b4bc --image dockhand-base-tahoe`. The branch deliberately does not exist. Binding validates the image first, then exits with the expected missing-branch error before accepting a job. This isolates image-validation/startup overhead; it does not measure full source binding, VM provisioning, or build duration. Each group shares its own database. The four concurrent warm invocations start together after a completed cache fill.

| Condition | Wall time |
| --- | ---: |
| Baseline, first process | 50.950 s |
| Baseline, second process | 48.263 s |
| Persistent cache, cold | 51.072 s |
| Persistent cache, new warm process | 0.260 s |
| Four concurrent warm processes, individual times | 0.130–0.133 s |

[Raw probe measurements](2026-09-13-verification-overhead.jsonl) retain invocation timings and expected errors.

Cold hashing remains expensive. An unchanged image avoids that cost on subsequent invocations, including invocations against another repository sharing the same database. These are a small number of observations, not latency percentiles. OS and storage caches affect timings. Concurrent cold misses can duplicate hashing; this implementation intentionally does not add coordination around a disposable cache.

Disk and NVRAM metadata changes invalidate the cache, including file replacement and change time even when modification time is restored. Configuration contents are read and hashed on every lookup; changed bytes invalidate the cache even with unchanged size and modification time. The stamp is checked before and after hashing and before returning a cached digest. A killed process cannot publish a partial hash; the next process can reuse only a completed matching row. As before, this assumes ordinary local filesystem metadata and a stopped prepared image; it is not an adversarial file-integrity monitor or an image lock.

## Observation and deadlines

Normal running-build provider observations now schedule ten seconds apart instead of one second. The due time is persisted and respected by competing drivers. CLI status/log refresh remains one second. A completed build can therefore take up to one observation interval to be noticed when drivers are otherwise idle; cancellation removes the normal observation delay while preserving active claims. This is a configured scheduling change, not a measured tenfold end-to-end speedup.

| Operation | Default call budget | Default claim lifetime |
| --- | ---: | ---: |
| Release resolution | 5 min | 5 min 30 s |
| Preparation and integration | 10 min | 10 min 30 s |
| Provisioning and reconciliation | 5 min | 5 min 30 s |
| Observation and capability lookup | 30 s | 1 min for claimed observations |
| Publication | 2 min | 2 min 30 s |
| Cancellation and cleanup | 1 min | 1 min 30 s |

The thirty-second grace period permits recording results after an operation returns. Expiry still requires reconciliation of uncertain external effects. Detached builds have no deadline derived from these call budgets.

## Validation

`make test-race`, `make vet`, and `CGO_ENABLED=0 make build` passed. The race suite includes cross-process cache reads/writes, metadata invalidation, interruption, migration rollback, competing observation claims, persisted scheduling, cancellation, and deadline/retry behavior.

A fresh verification of chezmoi 2.72.2 on `dockhand-base-tahoe` completed successfully in 392.36 seconds, including cold image validation, provisioning, indexing, dependency installation, the debug build, and cleanup. A second independent `dockhand wait` process attached during the run. Both exited zero and reported the same completed job, one attempt, a passing verdict, and released resource.

- Job: `job_PM6PLAIL2CDEZJNUWF4H7IUX7S`
- Attempt: `attempt_DHXXNPLSNBJNUMODHDVE2W2WBS`
- Source tree: `3ed0819e09a3a7b86592f4331b9b7ade62962ca9`
- Candidate: `cfae63efd80f27bd307a3008f1f80dc4f146a3d3`
- Accepted: 23:31:24 UTC; admitted: 23:32:01 UTC; completed: 23:36:56 UTC; release confirmed in the same second.

The live run used the initial metadata stamp. A subsequent nominally warm probe took 57.866 seconds because `config.json` change time had moved while its bytes and modification time stayed unchanged. Listing VMs alone did not reproduce the change. Tart uses that config file for [running-state lock checks](https://github.com/cirruslabs/tart/blob/2.36.0/Sources/tart/PIDLock.swift), but the exact modifying operation was not established. The final stamp reads the small config directly rather than relying on its change time. This preserves the original build-input content digest. A regression test covers both harmless timestamp updates and real same-size edits with restored modification time. Final real-image probes used the corrected cache and the default shared database:

| Condition | Wall time |
| --- | ---: |
| First lookup with the corrected stamp | 46.468 s |
| New warm process | 0.133 s |
| New process after creating a real Tart clone | 0.116 s |

The disposable clone was deleted afterward. The final full race suite passed after this correction; vet and the pure-Go build passed as well. The VM build ran before the stamp correction, while the subsequent real-clone probe specifically exercised the final image-checking path. The base disk is 100 GB logically; cached lookup avoids reading those bytes again when the content stamp still applies.
