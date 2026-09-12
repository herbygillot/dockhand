# Ledger performance baseline — 2026-09-12

The current ledger becomes expensive well before 10,000 distinct retained sources. At 1,000 completed jobs with distinct sources, changing one record took a median **8.58 seconds**, versus **168 milliseconds** when those jobs shared one source. At 10,000 distinct sources, even an update that changed nothing exhausted the store's **30-second operation timeout**.

Two mechanisms dominate: repeated per-source Git invocations while holding the writer lock, and workflow cycles opening transactions for completed jobs and released resources. Git ancestry depth alone did not materially change latency in the controlled experiment. No production implementation was changed for this study.

## Environment and scope

- Production baseline: `00a6b6b35dcb` in `~/Source/dockhand2`; only performance tooling, fixtures, tests, and reports were added.
- Apple M5 Max, 18 logical CPUs, 128 GiB RAM; macOS 26.6.2 (25G83), local APFS storage.
- Go 1.27.1, Git 2.55.0, SHA-1 fixture repositories.
- 24 sequential experiments: **640 uninstrumented samples and 35 diagnostic samples**. The experiments themselves took approximately 16 minutes in aggregate, excluding development, checks, and analysis.
- Normal ledger operations used the existing Git/fsync configuration, five-second writer-lock timeout, and thirty-second operation timeout. The harness supplied a 45-second outer deadline except for explicitly labeled ten-second cycle probes.

The fixtures contain one change, revision, request index, verification plan, attempt, and resource per job. Completed attempts have build/test evidence and an external log reference; their resources are released. Active jobs reuse the selected source pool. They use an instant, deterministic provider with no ledger probes. No MacPorts evaluation, actual builds, network calls, PR work, or real ports-tree modifications occur.

Fixed-size samples reset to the same ledger snapshot, remove newly added pins, warm-read the snapshot, and run Go GC before timing. Source objects and the initial state are generated outside timing. Most successful cases have three or five repetitions; the costly 1,000-source workflow case has one. These are exploratory warm-cache measurements, not tail-latency guarantees. Go GC before sampling also affects reusable buffer pools; allocation totals are not steady-state or peak-memory measurements. The machine was not reserved exclusively for benchmarking.

[Reproduction guide](/Users/herby/Source/dockhand2/tools/ledgerperf/README.md) · [Full results](/Users/herby/Source/dockhand2/docs/performance/2026-09-12/summary.csv) · [Diagnostic phases](/Users/herby/Source/dockhand2/docs/performance/2026-09-12/phases.csv)

## Small ledger operations

Elapsed milliseconds, medians of successful uninstrumented samples. Each row contains the stated completed jobs plus one active job.

| Completed jobs | Read, shared source | Update, shared source | Read, distinct sources | Update, distinct sources |
| ---: | ---: | ---: | ---: | ---: |
| 10 | 29.0 | 93.1 | 37.9 | 183.8 |
| 100 | 30.8 | 104.0 | 32.2 | 849.3 |
| 1,000 | 57.9 | 168.2 | 50.9 | 8,580.0 |
| 10,000 | 213.8 | 630.6 | 246.6 | **Timed out at 30,001 ms** |

The update changes one existing job's detail. A no-op transaction with 1,000 distinct sources still took **8.70 seconds**. At 10,000 distinct sources, no-op, edit, and new-revision operations all failed during `git rev-parse` source validation at the internal thirty-second limit. Their outer deadline was 45 seconds. Each failure is retained in the raw results; subsequent repetitions of that failing case were skipped.

Reads and status do not perform source-pin validation. At 10,000 jobs sharing a source, all-jobs status took 242 ms. The current snapshot was **33.5 MB of JSON**. A read allocated about 182 MB in the parent Go process and a small update allocated about **481 MB**. Those are cumulative allocations during an operation, not resident or peak memory, and exclude child-process memory. Whole-state processing is still a material cost after source validation is addressed.

![Ledger latency](/Users/herby/Source/dockhand2/docs/performance/2026-09-12/ledger-latency.png)

## Where a write spends its time

A separate diagnostic build instruments existing calls through a temporary Go build overlay. These diagnostic timings explain the uninstrumented results but are not substituted for them.

For one edit with 1,000 distinct sources:

| Measured component | Time |
| --- | ---: |
| Waiting for the uncontended lock | 0.08 ms |
| Reading and decoding the current snapshot | 59.5 ms |
| Encoding/validating before and after the callback | 19.1 ms |
| Source-pin processing | **7,782.9 ms** |
| Of that pin processing: 1,000 `git rev-parse` calls | **7,649.9 ms** |
| Final `git update-ref` command | 369.7 ms |
| Total time holding the writer lock | **8,291.5 ms** |

The operation launched **1,010 Git subprocesses**. Source-pin processing occupied about **94%** of lock ownership. The component rows overlap where explicitly indicated and are not an additive accounting of every instruction.

The corresponding shared-source edit launched 11 Git subprocesses and held the lock for 156 ms in its diagnostic sample. At 1,000 distinct sources, a no-op transaction launched 1,006 subprocesses and held the lock for 7.90 seconds despite publishing no new state commit.

## Workflow multiplication

Elapsed seconds for a cycle scoped to one active job. The provider itself returns immediately.

| Completed jobs | Distinct sources | Submit a job | Observe still running | Complete and release |
| ---: | ---: | ---: | ---: | ---: |
| 100 | 1 | 0.100 | 0.406 | 0.563 |
| 100 | 100 | 0.833 | 3.312 | 4.229 |
| 1,000 | 1,000 | 7.721 | **30.918** | **38.733** |
| 10,000 | 1 | 0.604 | 2.703 | 3.507 |

The 1,000-distinct-source row is one sample per operation; the other rows are medians. An overall cycle may exceed thirty seconds because that limit applies to each ledger operation separately.

A running-observation cycle used **four transactions**: a no-op control pass, an attempt claim, adoption of the provider observation, and a no-op cleanup check for the active resource. It also read status three times. With 1,000 distinct sources, the diagnostic cycle launched **4,044 Git subprocesses**, including 4,000 repeated commit/tree lookups.

With **ten active jobs** and 100 completed jobs, a selected-jobs observation cycle took a median **2.84 seconds** with one shared source and **27.57 seconds** with 100 distinct sources. A ten-second observation schedule could not be sustained by that latter measured cycle even with an instant provider.

An all-jobs cycle adds work for retained terminal records:

| Completed jobs | Distinct sources | Active jobs | Idle cycle |
| ---: | ---: | ---: | ---: |
| 10 | 1 | 0 | 1.134 s |
| 10 | 10 | 0 | 2.566 s |
| 100 | 1 | 0 | **11.605 s** |
| 100 | 100 | 0 | Stopped at the experimental 10 s cutoff |
| 1,000 | 1 | 0 | Stopped at the experimental 10 s cutoff |
| 10,000 | 1 | 0 | Stopped at the experimental 10 s cutoff |

The 100-job/shared-source case was rerun with a 45-second budget to obtain its completed duration. The other cutoffs are lower bounds, not the store's default timeout and not estimates of completed-cycle duration.

For just ten completed jobs and ten released resources, the diagnostic idle cycle opened **21 no-op transactions**. It launched **159 Git commands** with a shared source, or **348** with ten distinct sources. The implementation revisits every selected terminal job and released resource through a transaction. When retained records and distinct sources grow together, this nesting makes the Git command count grow quadratically.

## Contention

Separate OS processes opened the same ledger and lockfile, waited at a common start barrier, then issued successive operations. Each writer attempted ten updates in the 100-source cases, or four updates in the 1,000-source case. One reader issued the same number of reads at the beginning of each burst.

| Completed jobs / sources | Writers | Successful writes / attempts | Lock timeouts | Median successful write | Median read |
| --- | ---: | ---: | ---: | ---: | ---: |
| 100 / 100 | 1 | 10 / 10 | 0 | 0.854 s | 30.5 ms |
| 100 / 100 | 2 | 19 / 20 | **1** | 0.892 s | 29.8 ms |
| 100 / 100 | 4 | 31 / 40 | **9** | 0.914 s | 39.4 ms |
| 1,000 / 1,000 | 2 | 4 / 8 | **4** | 8.398 s | 62.8 ms |

Failed writes reached the five-second lock-acquisition limit. The successful-write median excludes those failures and must be read with the failure counts. Successive callers can reacquire the lock while another waits; a sequence of sub-second writes can therefore cause a five-second waiter timeout.

This was a closed-loop burst, not a calibrated arrival-rate capacity test. Readers generally finished before writers, so these read results describe the beginning of contention rather than continuous monitoring throughout the burst. Every read succeeded. The lock-free snapshot design is providing a useful separation between read responsiveness and writer contention.

## History, packing, and storage

Holding current state fixed at 100 completed jobs, one active job, and one source isolated ledger ancestry depth:

| Extra historical commits | Post-seed Git GC | Median read | Median edit |
| ---: | --- | ---: | ---: |
| 0 | No | 30.8 ms | 104.0 ms |
| 1,000 | No | 29.3 ms | 98.4 ms |
| 10,000 | No | 30.7 ms | 99.1 ms |
| 10,000 | Yes | 30.2 ms | 99.2 ms |

Those synthetic historical commits share a state tree. This isolates ancestry cost; it does not simulate growing record collections or accumulating full snapshots. No meaningful history-depth slowdown appeared at these sizes. The costly history is primarily the records and sources retained in the **current snapshot**, not traversal of the ledger commit chain.

Packing the 1,000-distinct-source fixture left an edit at **8.21 seconds**, versus 8.58 seconds without post-seed GC. This comparison does not establish a significant packing speedup; it shows packing does not remove the dominant per-source work. Fast-import may itself pack source objects during setup, so the baseline is not claimed to contain only loose objects.

Real successive edits, with no resetting, showed a separate storage effect:

| Completed jobs, one shared source | Initial Git directory | After 100 actual updates | After Git GC |
| --- | ---: | ---: | ---: |
| 10 | 31,601 B | 352,140 B | 95,783 B |
| 1,000 | 147,650 B | **12,073,741 B** | **631,992 B** |

These totals sum regular-file lengths under the Git directory; they are not allocated filesystem blocks or bytes written to disk. Each experiment ended with 309 Git objects, and packing retained all 309. Both retained two source pins throughout. Git delta compression substantially reduced storage for similar snapshots, while the application still serialized and wrote a whole snapshot per edit. Source churn and long-term pin retention require a separate retention decision; this short fixed-source growth experiment cannot establish their eventual storage bound.

![Ledger storage](/Users/herby/Source/dockhand2/docs/performance/2026-09-12/ledger-storage.png)

## Implications

1. **Batch commit/tree validation first.** The dominant measured cost is one subprocess per retained source commit. This can be addressed while preserving commit/tree checks and atomic source retention. Re-measure before deciding whether validation caching or broader storage changes are necessary.
2. **Avoid transactions for known ineligible workflow entries.** Preselect actionable jobs/resources and pending controls from the snapshot, then recheck selected work under the transaction. Preserve the recovery and pin-repair guarantees; do not simply delete those checks. The idle-cycle results justify treating this as an immediate performance concern.
3. **Measure the same cases again after those changes.** Shorter lock ownership should improve both CLI latency and contention. Increasing timeouts alone would leave the repeated work and waiting intact.
4. **Then address whole-state processing and retention using the remaining profile.** The 33.5 MB snapshot and approximately 481 MB allocated per edit at 10,000 jobs are meaningful even with one source. Current-state size, retained Git history, and source pins need distinct policies; the evidence does not make history compaction the first latency fix.

## Reproduction and artifacts

`tools/ledgerperf/README.md` documents fixture construction, timing boundaries, commands, diagnostic overlays, and limitations. The adjacent `2026-09-12/` directory contains raw JSONL samples, diagnostic event logs, exact command manifests, `summary.csv`, `phases.csv`, and latency/storage plots. Failed measurements are included, not discarded. Diagnostic phase rows overlap and must not be summed indiscriminately.

Validation: `go test -race ./...` and `go vet ./...` passed. The new standard Go benchmarks were also smoke-tested for ledger and workflow cases at ten completed jobs. Those smoke-test timings are excluded from the baseline because checks and plotting preparation were allowed to run concurrently after the controlled experiment sequence.
